package meeting

import (
	"context"
	"errors"
	"sync"
	"testing"

	"voicerewrite/internal/core/audio"
)

func TestCaptureRouteTraceAndEpochFence(t *testing.T) {
	s := NewSessionRuntime()
	_, epoch, _ := s.Snapshot()
	trace := []struct {
		event      CaptureEvent
		source     string
		expected   uint64
		wantError  error
		wantActive bool
	}{
		{CaptureStart, "app-A", 0, nil, true},
		{CaptureStart, "app-A", 0, ErrCaptureActive, true},
		{CapturePause, "", epoch, nil, false},
		{CaptureResume, "", epoch, ErrCaptureEpoch, false},
	}
	for _, step := range trace {
		_, err := s.CaptureRoute(step.event, step.source, step.expected)
		if !errors.Is(err, step.wantError) {
			t.Fatalf("%s: error=%v want %v", step.event, err, step.wantError)
		}
		if got := s.Active(epoch); got != step.wantActive && step.event != CapturePause {
			t.Fatalf("%s: stale epoch unexpectedly active=%v", step.event, got)
		}
	}
	_, pausedEpoch, paused := s.Snapshot()
	if !paused || pausedEpoch <= epoch {
		t.Fatal("pause did not cancel epoch")
	}
	resumed, err := s.CaptureRoute(CaptureResume, "", pausedEpoch)
	if err != nil || !s.Active(resumed) || resumed <= pausedEpoch {
		t.Fatalf("resume: epoch=%d err=%v", resumed, err)
	}
	switched, err := s.CaptureRoute(CaptureSwitch, "app-B", resumed)
	if err != nil || switched <= resumed || s.CaptureSource() != "app-B" {
		t.Fatalf("source switch: epoch=%d err=%v", switched, err)
	}
	if s.Active(resumed) {
		t.Fatal("old capture published after source switch")
	}
	if _, err := s.CaptureRoute(CaptureLost, "", resumed); !errors.Is(err, ErrCaptureEpoch) {
		t.Fatalf("late source failure canceled new source: %v", err)
	}
	lost, err := s.CaptureRoute(CaptureLost, "", switched)
	if err != nil || s.Active(lost) {
		t.Fatalf("source loss not fenced: %v", err)
	}
	if _, err := s.CaptureRoute(CaptureResume, "", lost); !errors.Is(err, ErrCaptureSource) {
		t.Fatalf("lost source resumed without replacement: %v", err)
	}
	next, err := s.CaptureRoute(CaptureSwitch, "app-C", lost)
	if err != nil {
		t.Fatal(err)
	}
	final, err := s.CaptureRoute(CaptureResume, "", next)
	if err != nil || !s.Active(final) {
		t.Fatalf("new source failed to resume: %v", err)
	}
	_, err = s.CaptureRoute(CaptureStop, "", final)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CaptureRoute(CaptureStart, "app-D", 0); !errors.Is(err, ErrCaptureStopped) {
		t.Fatalf("stopped session restarted: %v", err)
	}
}

func TestBoundedPacketPolicy(t *testing.T) {
	for _, tc := range []struct {
		kind         QueueKind
		count, limit int
		want         QueueAction
	}{
		{QueuePacket, 7, 8, QueueAppend},
		{QueuePacket, 8, 8, QueueDropIncoming},
		{QueuePacket, 0, 0, QueueDropIncoming},
		{QueuePreview, 1, 1, QueueReplaceOldest},
		{QueueFinal, 2, 2, QueueReplaceOldest},
		{QueueCaption, 1, 1, QueueReplaceOldest},
	} {
		if got := DecideQueue(tc.kind, tc.count, tc.limit); got != tc.want {
			t.Errorf("decision %v %d/%d = %v, want %v", tc.kind, tc.count, tc.limit, got, tc.want)
		}
	}
	s := NewSessionRuntime()
	defer s.Stop()
	epoch, err := s.CaptureRoute(CaptureStart, "source", 0)
	if err != nil {
		t.Fatal(err)
	}
	if action, err := s.AdmitPCM(epoch, audio.FrameBytes, 0, PacketBacklog); action != QueueAppend || err != nil {
		t.Fatalf("normal PCM: %v, %v", action, err)
	}
	if action, err := s.AdmitPCM(epoch, audio.FrameBytes, PacketBacklog, PacketBacklog); action != QueueDropIncoming || err != nil {
		t.Fatalf("full queue: %v, %v", action, err)
	}
	if _, err := s.AdmitPCM(epoch, audio.FrameBytes+1, 0, PacketBacklog); err == nil {
		t.Fatal("accepted non-20ms PCM")
	}
	if _, err := s.CaptureRoute(CapturePause, "", epoch); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdmitPCM(epoch, audio.FrameBytes, 0, PacketBacklog); err == nil {
		t.Fatal("paused PCM admitted")
	}
	next, _ := s.CaptureRoute(CaptureResume, "", 0)
	if _, err := s.AdmitPCM(epoch, audio.FrameBytes, 0, PacketBacklog); !errors.Is(err, ErrCaptureEpoch) {
		t.Fatalf("stale PCM: %v", err)
	}
	if _, err := s.AdmitPCM(next, audio.FrameBytes, 0, PacketBacklog); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureRouteConcurrentStopPauseAndFeed(t *testing.T) {
	s := NewSessionRuntime()
	epoch, err := s.CaptureRoute(CaptureStart, "source", 0)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_, _ = s.AdmitPCM(epoch, audio.FrameBytes, j%PacketBacklog, PacketBacklog)
				if i%2 == 0 {
					_, _ = s.CaptureRoute(CapturePause, "", 0)
				}
				if i%3 == 0 {
					_, _ = s.CaptureRoute(CaptureResume, "", 0)
				}
			}
		}(i)
	}
	s.Stop()
	wg.Wait()
	if s.Context().Err() != context.Canceled {
		t.Fatal("stop did not cancel capture")
	}
	if _, err := s.AdmitPCM(epoch, audio.FrameBytes, 0, PacketBacklog); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-stop feed accepted: %v", err)
	}
}
