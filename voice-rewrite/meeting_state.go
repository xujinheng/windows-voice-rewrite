package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"
)

type meetingCaption struct {
	id                            uint64
	source, chinese, stable, live string
	final                         bool
}

type meetingMetrics struct {
	Stage                                                        string
	Pending, Dropped                                             int
	ASRFirstMS, ASRDoneMS, TranslationFirstMS, TranslationDoneMS int64
}

type meetingView struct {
	status                             string
	captions                           []meetingCaption
	replies                            [3]meetingLine
	tip, replySource, expressionStatus string
	expression                         meetingLine
	sourceName, asrMode                string
	level                              int
	capturePaused                      bool
	metrics                            meetingMetrics
	pendingReplies                     [3]meetingLine
	pendingReplySource, pendingTip     string
	replyPending                       bool
}

func reviseMeetingCaption(old meetingCaption, id uint64, text string, final bool) meetingCaption {
	if old.id == id && old.final {
		return old
	}
	out := meetingCaption{id: id, source: text, final: final}
	if old.id == id {
		// Keep the last readable translation until the replacement arrives.
		out.chinese = old.chinese
	}
	if final {
		out.stable = text
		return out
	}
	if old.id == id {
		a, b := []rune(old.source), []rune(text)
		n := 0
		for n < len(a) && n < len(b) && a[n] == b[n] {
			n++
		}
		// Keep the last word (or four Chinese characters) provisional.
		end := n
		for end > 0 && !unicode.IsSpace(b[end-1]) {
			end--
		}
		if end == 0 && n > 4 {
			end = n - 4
		}
		if end > 0 {
			out.stable = string(b[:end])
		}
		if strings.HasPrefix(text, old.stable) && len([]rune(old.stable)) > len([]rune(out.stable)) {
			out.stable = old.stable
		}
	}
	out.live = strings.TrimPrefix(text, out.stable)
	return out
}

// meetingTranscript is one continuous reading surface. An unfinished line stays
// at the bottom of the timeline instead of jumping between two text boxes.
func meetingTranscript(captions []meetingCaption) (string, bool) {
	var entries []string
	listening := false
	for _, line := range captions {
		source := strings.TrimSpace(line.source)
		if source == "" {
			continue
		}
		translation := strings.TrimSpace(line.chinese)
		if line.final {
			if translation != "" {
				entries = append(entries, translation+"\r\n"+source)
			} else {
				entries = append(entries, source)
			}
			continue
		}
		listening = true
		if translation != "" {
			entries = append(entries, translation+"\r\n"+source+"  · 识别中")
		} else {
			entries = append(entries, source+"  · 识别中")
		}
	}
	if len(entries) == 0 {
		return "等待会议音频…", false
	}
	return strings.Join(entries, "\r\n\r\n"), listening
}

func meetingFollowLatest(current, moved, atBottom bool) bool {
	if moved {
		return atBottom
	}
	return current
}

// meetingTextPatch describes the smallest end-anchored change to a Windows
// multiline edit control. Its indexes use UTF-16 code units, not Go bytes.
type meetingTextPatch struct {
	start, end int
	insert     string
}

func meetingTextChange(old, next string) meetingTextPatch {
	a, b := []rune(old), []rune(next)
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return meetingTextPatch{
		start: len(utf16.Encode(a[:n])),
		end:   len(utf16.Encode(a)),
		insert: string(b[n:]),
	}
}

// Shift saved selections past the changed tail, without pointing into deleted
// text or disrupting an existing selection in the stable transcript prefix.
func meetingMapSelection(pos uint32, patch meetingTextPatch) uint32 {
	at := int(pos)
	if at <= patch.start {
		return pos
	}
	if at < patch.end {
		return uint32(patch.start)
	}
	inserted := len(utf16.Encode([]rune(patch.insert)))
	return uint32(at + inserted - (patch.end - patch.start))
}

func meetingShouldReply(latest string, prior []string, semantic bool) bool {
	text := strings.Trim(strings.ToLower(strings.TrimSpace(latest)), ".!?？。，")
	switch text {
	case "", "yeah", "yes", "no", "okay", "ok", "uh-huh", "thanks", "thank you", "好的", "嗯", "谢谢":
		return false
	}
	return meetingNeedsReply(latest) || semantic
}

const (
	meetingPriorityExpress = iota
	meetingPriorityASR
	meetingPriorityCaption
	meetingPriorityPreview
	meetingPriorityReply
)

var errMeetingPreempted = errors.New("meeting inference yielded to higher priority work")

type meetingWaiter struct {
	priority int
	ctx      context.Context
	run      context.Context
	cancel   context.CancelCauseFunc
	ready    chan struct{}
}

// Cancellation requests preemption; ownership changes only on release, after
// the old HTTP call has returned. This prevents overlap on a shared GPU.
type meetingScheduler struct {
	mu      sync.Mutex
	active  *meetingWaiter
	waiters []*meetingWaiter
}

func (g *meetingScheduler) acquire(ctx context.Context, priority int) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	run, cancel := context.WithCancelCause(ctx)
	w := &meetingWaiter{priority: priority, ctx: ctx, run: run, cancel: cancel, ready: make(chan struct{})}
	g.mu.Lock()
	g.waiters = append(g.waiters, w)
	if g.active != nil && (priority == meetingPriorityExpress || g.active.priority == meetingPriorityReply) && priority < g.active.priority {
		g.active.cancel(errMeetingPreempted)
	}
	g.advanceLocked()
	g.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			g.mu.Lock()
			cancel(context.Canceled)
			if g.active == w {
				g.active = nil
			}
			for i, q := range g.waiters {
				if q == w {
					g.waiters = append(g.waiters[:i], g.waiters[i+1:]...)
					break
				}
			}
			g.advanceLocked()
			g.mu.Unlock()
		})
	}
	select {
	case <-ctx.Done():
		release()
		return nil, nil, ctx.Err()
	case <-w.ready:
		if err := ctx.Err(); err != nil {
			release()
			return nil, nil, err
		}
		return run, release, nil
	}
}
func (g *meetingScheduler) advanceLocked() {
	if g.active != nil {
		return
	}
	for len(g.waiters) > 0 {
		best := 0
		for i := range g.waiters {
			if g.waiters[i].priority < g.waiters[best].priority {
				best = i
			}
		}
		w := g.waiters[best]
		g.waiters = append(g.waiters[:best], g.waiters[best+1:]...)
		if w.ctx.Err() != nil {
			w.cancel(w.ctx.Err())
			continue
		}
		g.active = w
		close(w.ready)
		return
	}
}
func (g *meetingScheduler) pending() int { g.mu.Lock(); defer g.mu.Unlock(); return len(g.waiters) }
