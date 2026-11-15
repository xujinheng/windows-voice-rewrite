package main

import (
	"context"
	"errors"
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
