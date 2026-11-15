package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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

func TestMeetingTextChangeUTF16AndHistory(t *testing.T) {
	cases := []struct {
		old, next string
		start, end int
		insert string
	}{
		{"", "你好", 0, 0, "你好"},
		{"第一句\r\nHello", "第一句\r\nHello world", 10, 10, " world"},
		{"🙂 原文\r\n第一版", "🙂 原文\r\n第二版", 8, 10, "二版"},
	}
	for _, tc := range cases {
		p := meetingTextChange(tc.old, tc.next)
		if p.start != tc.start || p.end != tc.end || p.insert != tc.insert {
			t.Errorf("%q -> %q: got %+v", tc.old, tc.next, p)
		}
	}
	patch := meetingTextChange("标题\r\n第一句", "标题\r\n第一句\r\n第二句")
	if meetingMapSelection(2, patch) != 2 {
		t.Fatal("existing selection moved while appending")
	}
}

func TestMeetingFollowLatestIsUserControlled(t *testing.T) {
	if !meetingFollowLatest(true, false, false) {
		t.Fatal("background update disabled following")
	}
	if meetingFollowLatest(true, true, false) {
		t.Fatal("manual upward scrolling did not stop following")
	}
	if meetingFollowLatest(false, false, true) {
		t.Fatal("background update resumed following without scroll")
	}
	if !meetingFollowLatest(false, true, true) {
		t.Fatal("returning to the bottom did not resume following")
	}
}

func TestMeetingSchedulerPreemptsReplyAndWaitsForRelease(t *testing.T) {
	var g meetingScheduler
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	low, release, err := g.acquire(ctx, meetingPriorityReply)
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	go func() {
		_, done, e := g.acquire(ctx, meetingPriorityExpress)
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
	if !errors.Is(context.Cause(low), errMeetingPreempted) {
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
	var g meetingScheduler
	ctx, release, e := g.acquire(context.Background(), meetingPriorityExpress)
	_ = ctx
	if e != nil {
		t.Fatal(e)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e = g.acquire(canceled, meetingPriorityCaption); e == nil {
		t.Fatal("accepted canceled job")
	}
	release()
	if g.pending() != 0 {
		t.Fatal("leaked waiter")
	}
}

func TestMeetingGateContext(t *testing.T) {
	for _, s := range []string{"Let's hear your take", "Can we hear from you", "你怎么看", "Any concerns about that approach"} {
		if !meetingNeedsReply(s) {
			t.Errorf("missed handoff %q", s)
		}
	}
	if meetingShouldReply("yeah", []string{"What do you think?"}, true) {
		t.Fatal("backchannel triggered reply")
	}
	if !meetingShouldReply("That is the constraint we are working with", nil, true) {
		t.Fatal("semantic gate ignored")
	}
}
