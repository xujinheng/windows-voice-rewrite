package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	meetingcore "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/meeting"
)

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
	if g.Pending() != 0 {
		t.Fatal("leaked waiter")
	}
}

func TestMeetingExpressionRemainsReadableWhileWorking(t *testing.T) {
	if !meetingcore.KeepFinishedEnglish(true, "I would add one more point.") ||
		meetingcore.KeepFinishedEnglish(false, "Half a sentence") {
		t.Fatal("completed English must remain copyable, but not partial English")
	}
	if heading := meetingcore.ExpressionHeading("正在识别", true); !strings.Contains(heading, "仍可复制") {
		t.Fatalf("the interface concealed the last good English: %s", heading)
	}
	if detail := meetingcore.ExpressionDetail("生成失败：超时", "印度有多少人会说英语"); !strings.Contains(detail, "超时") || !strings.Contains(detail, "印度") {
		t.Fatalf("failure hid the recognized speech: %s", detail)
	}
	copy, focusedCaptions, expanded, fresh, _ := meetingcore.ExpressionStep(true, "done", "previous", true, false, time.Time{}, time.Now())
	if copy != "done" || !fresh || focusedCaptions || !expanded {
		t.Fatal("completed English was hidden behind captions-only mode")
	}
	_, focusedCaptions, expanded, fresh, _ = meetingcore.ExpressionStep(true, "done", "done", true, false, time.Time{}, time.Now())
	if fresh || !focusedCaptions || expanded {
		t.Fatal("a background refresh stole the user's caption focus")
	}
}
