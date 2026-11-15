package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	meetingcore "voicerewrite/internal/core/meeting"
)

func TestMeetingStableTail(t *testing.T) {
	var c meetingCaption
	c = reviseMeetingCaption(c, 1, "We should check the cache", false)
	if c.stable != "" || c.live == "" {
		t.Fatal(c)
	}
	c = reviseMeetingCaption(c, 1, "We should check the cache first", false)
	if c.stable != "We should check the " || c.live != "cache first" {
		t.Fatalf("%#v", c)
	}
	c = reviseMeetingCaption(c, 1, "We should check the cache first.", true)
	final := c
	c = reviseMeetingCaption(c, 1, "stale preview", false)
	if c != final || !c.final || c.live != "" {
		t.Fatal(c)
	}
}

func TestMeetingTranscriptKeepsStableText(t *testing.T) {
	base := meetingCaption{id: 4, source: "We should check", chinese: "我们应该检查"}
	revised := reviseMeetingCaption(base, 4, "We should check the index", false)
	if revised.chinese != base.chinese {
		t.Fatalf("translation vanished during ASR: %#v", revised)
	}
	text, live := meetingTranscript([]meetingCaption{
		{id: 3, source: "Earlier response", chinese: "之前的回复", final: true},
		revised,
	})
	if !live || !strings.Contains(text, "之前的回复") || !strings.Contains(text, "我们应该检查") || !strings.Contains(text, "We should check the index") {
		t.Fatalf("transcript lost content: %q (%t)", text, live)
	}
	revised = reviseMeetingCaption(revised, 4, "We should check the index.", true)
	if revised.chinese != base.chinese {
		t.Fatalf("finalization removed the translation: %#v", revised)
	}
	text, live = meetingTranscript([]meetingCaption{revised})
	if live || strings.Contains(text, "识别中") {
		t.Fatalf("final transcript remains provisional: %q", text)
	}
}

func TestMeetingReadingPanelsKeepActiveTurnOutOfHistory(t *testing.T) {
	captions := []meetingCaption{
		{id: 1, source: "We can start today.", chinese: "今天可以开始。", final: true},
		{id: 2, source: "Do we have a timeline?", chinese: "有时间计划吗？", final: true},
		{id: 3, source: "The schedule", final: false},
	}
	panels := buildMeetingReadingPanels(captions)
	if panels.english != "The schedule" || panels.chinese != "正在准备中文释义…" || !panels.live {
		t.Fatalf("current provisional reading area = %#v", panels)
	}
	if panels.historyCount != 2 || strings.Contains(panels.history, "The schedule") ||
		!strings.Contains(panels.history, "今天可以开始。") || !strings.Contains(panels.history, "Do we have a timeline?") {
		t.Fatalf("history must contain completed older turns only: %#v", panels)
	}
	captions[2].source = "The schedule is next week."
	captions[2].chinese = "计划安排在下周。"
	changed := buildMeetingReadingPanels(captions)
	if changed.history != panels.history || changed.historyCount != panels.historyCount {
		t.Fatal("active translation changed scrollable history")
	}
	if changed.chinese != "计划安排在下周。" {
		t.Fatalf("translation wasn't updated in fixed pane: %#v", changed)
	}
	captions[2].final = true
	last := buildMeetingReadingPanels(captions)
	if last.live || last.english != changed.english || last.history != panels.history {
		t.Fatalf("finalizing current turn moved the reader: %#v", last)
	}
	captions = append(captions, meetingCaption{id: 4, source: "What is the deadline?", final: true})
	next := buildMeetingReadingPanels(captions)
	if next.historyCount != 3 || !strings.Contains(next.history, "The schedule is next week.") ||
		strings.Contains(next.history, "What is the deadline?") {
		t.Fatalf("historical turn assignment: %#v", next)
	}
	if next.hint != "原文已确认 · 正在生成译文" {
		t.Fatalf("caption state must not guess conversational intent: %#v", next)
	}
}

func TestMeetingReadingPanelsEmptyOrLongSession(t *testing.T) {
	if got := buildMeetingReadingPanels(nil); got.historyCount != 0 || got.english == "" || got.chinese == "" {
		t.Fatalf("empty layout did not provide placeholders: %#v", got)
	}
	var captions []meetingCaption
	for i := 0; i < 60; i++ {
		captions = append(captions, meetingCaption{source: "A recorded turn", final: true})
	}
	got := buildMeetingReadingPanels(captions)
	if got.historyCount != 59 || strings.Count(got.history, "A recorded turn") != 59 {
		t.Fatalf("long-session history discarded: %#v", got)
	}
}

func TestMeetingUnreadHistoryDoesNotStealFocus(t *testing.T) {
	if meetingcore.UnreadHistory(0, 3, 5, false) != 2 {
		t.Fatal("missing unread count while reading old captions")
	}
	if meetingcore.UnreadHistory(2, 5, 6, false) != 3 {
		t.Fatal("unread count must accumulate only newly finished turns")
	}
	if meetingcore.UnreadHistory(3, 6, 6, false) != 3 {
		t.Fatal("current caption revisions must not count as new history")
	}
	if meetingcore.UnreadHistory(3, 6, 6, true) != 0 {
		t.Fatal("returning to newest should reset unread count")
	}
}

func TestMeetingTextChangeUTF16AndHistory(t *testing.T) {
	cases := []struct {
		old, next  string
		start, end int
		insert     string
	}{
		{"", "你好", 0, 0, "你好"},
		{"第一句\r\nHello", "第一句\r\nHello world", 10, 10, " world"},
		{"🙂 原文\r\n第一版", "🙂 原文\r\n第二版", 8, 10, "二版"},
	}
	for _, tc := range cases {
		p := meetingcore.TextChange(tc.old, tc.next)
		if p.Start != tc.start || p.End != tc.end || p.Insert != tc.insert {
			t.Errorf("%q -> %q: got %+v", tc.old, tc.next, p)
		}
	}
	patch := meetingcore.TextChange("标题\r\n第一句", "标题\r\n第一句\r\n第二句")
	if meetingcore.MapSelection(2, patch) != 2 {
		t.Fatal("existing selection moved while appending")
	}
}

func TestMeetingContextOnlyWhenSpeechRefersToPriorConversation(t *testing.T) {
	for _, spoken := range []string{
		"问他这两个方案的 schedule", "刚才说的时间安排", "他们说的那个方案",
	} {
		if !meetingcore.UsesConversationContext(spoken) {
			t.Errorf("missing reference to conversation: %q", spoken)
		}
	}
	for _, spoken := range []string{
		"我需要回答有多少印度人会说英语", "有多少印度人会说英语",
		"我没等他问，我想先谈谈教育问题", "我想说这部电影很有意思",
	} {
		if meetingcore.UsesConversationContext(spoken) {
			t.Errorf("unrelated prompt inherited project context: %q", spoken)
		}
	}
}

func TestMeetingVoiceModes(t *testing.T) {
	if meetingcore.VoiceMode(true) != "express" {
		t.Fatal("dictation key must translate speech literally")
	}
	if meetingcore.VoiceMode(false) != "compose" {
		t.Fatal("rewrite key must compose an English utterance from intent")
	}
}

func TestMeetingFollowLatestIsUserControlled(t *testing.T) {
	if !meetingcore.FollowLatest(true, false, false) {
		t.Fatal("background update disabled following")
	}
	if meetingcore.FollowLatest(true, true, false) {
		t.Fatal("manual upward scrolling did not stop following")
	}
	if meetingcore.FollowLatest(false, false, true) {
		t.Fatal("background update resumed following without scroll")
	}
	if !meetingcore.FollowLatest(false, true, true) {
		t.Fatal("returning to the bottom did not resume following")
	}
}

func TestMeetingSchedulerPreemptsReplyAndWaitsForRelease(t *testing.T) {
	var g meetingcore.Scheduler
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	low, release, err := g.Acquire(ctx, meetingcore.PriorityReply)
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	go func() {
		_, done, e := g.Acquire(ctx, meetingcore.PriorityExpress)
		if e == nil {
			close(ready)
			done()
		}
	}()
	select {
	case <-low.Done():
	case <-ctx.Done():
		t.Fatal("not preempted")
	}
	if !errors.Is(context.Cause(low), meetingcore.ErrPreempted) {
		t.Fatal(context.Cause(low))
	}
	select {
	case <-ready:
		t.Fatal("overlapping GPU requests")
	default:
	}
	release()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("priority request blocked")
	}
}

func TestMeetingSchedulerCanceledWaiterRemoved(t *testing.T) {
	var g meetingcore.Scheduler
	ctx, release, e := g.Acquire(context.Background(), meetingcore.PriorityExpress)
	_ = ctx
	if e != nil {
		t.Fatal(e)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e = g.Acquire(canceled, meetingcore.PriorityCaption); e == nil {
		t.Fatal("accepted canceled job")
	}
	release()
	if g.pending() != 0 {
		t.Fatal("leaked waiter")
	}
}

func TestMeetingTranslationVersionIsVisibleToReader(t *testing.T) {
	translated := meetingCaption{id: 1, source: "I can start Monday", chinese: "我周一可以开始", translatedFrom: "I can start Monday", final: true}
	ready := buildMeetingReadingPanels([]meetingCaption{translated})
	if ready.hint != "已确认 · 可以放心阅读" {
		t.Fatalf("completed translation incorrectly marked pending: %q", ready.hint)
	}
	changed := reviseMeetingCaption(meetingCaption{id: 1, source: "I can start Monday", chinese: "我周一可以开始", translatedFrom: "I can start Monday"}, 1, "I can start Tuesday", true)
	if changed.chinese != translated.chinese || changed.translatedFrom != translated.translatedFrom {
		t.Fatalf("changing ASR erased the last readable Chinese version: %#v", changed)
	}
	stale := buildMeetingReadingPanels([]meetingCaption{changed})
	if stale.hint != "原文已更新 · 译文待刷新" || stale.chinese != translated.chinese {
		t.Fatalf("stale translation shown as final: %#v", stale)
	}
}

func TestMeetingExpressionRemainsReadableWhileWorking(t *testing.T) {
	last := meetingLine{kind: "SAY", english: "I would add one more point.", complete: true}
	if meetingRetainEnglish(last) != last {
		t.Fatal("the last completed spoken English was lost")
	}
	if meetingRetainEnglish(meetingLine{english: "Half a sentence"}).english != "" {
		t.Fatal("uncommitted English must not survive a failed request")
	}
	if heading := meetingcore.ExpressionHeading("正在识别", true); !strings.Contains(heading, "仍可复制") {
		t.Fatalf("the interface concealed the last good English: %s", heading)
	}
	if detail := meetingcore.ExpressionDetail("生成失败：超时", "印度有多少人会说英语"); !strings.Contains(detail, "超时") || !strings.Contains(detail, "印度") {
		t.Fatalf("failure hid the recognized speech: %s", detail)
	}
	focusedCaptions, expanded := meetingcore.ExpressionFocus(true, true, true, false)
	if focusedCaptions || !expanded {
		t.Fatal("completed English was hidden behind captions-only mode")
	}
	focusedCaptions, expanded = meetingcore.ExpressionFocus(true, false, true, false)
	if !focusedCaptions || expanded {
		t.Fatal("a background refresh stole the user's caption focus")
	}
}

func TestMeetingSuggestionsUseCompletedSpeechOnly(t *testing.T) {
	if _, ok := meetingLastCompletedCaption([]meetingCaption{{id: 3, source: "What about", final: false}}); ok {
		t.Fatal("a partial fragment was used to generate suggestions")
	}
	finished, ok := meetingLastCompletedCaption([]meetingCaption{
		{id: 1, source: "Let's consider both.", final: true},
		{id: 2, source: "Maybe...", final: false},
	})
	if !ok || finished.id != 1 {
		t.Fatalf("suggestions ignored the latest completed turn: %#v %t", finished, ok)
	}
}
