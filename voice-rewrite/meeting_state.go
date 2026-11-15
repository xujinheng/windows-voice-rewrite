package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"unicode"
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
	if old.source == text {
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
	}
	out.live = strings.TrimPrefix(text, out.stable)
	return out
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
