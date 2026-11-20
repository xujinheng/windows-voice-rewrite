package voice

import (
	"testing"
	"time"
)

func TestVoiceStartKeepsSideAndKeyboardWindowsApart(t *testing.T) {
	if HoldModeLabel(true, true) != "会议直译" || HoldModeLabel(false, true) != "会议意图" || HoldModeLabel(true, false) != "听写" || HoldModeLabel(false, false) != "改写" {
		t.Fatal("mode")
	}
}

func TestSideHoldThresholdDuration(t *testing.T) {
	if SideHoldThreshold != 250*time.Millisecond {
		t.Fatalf("windows %s", SideHoldThreshold)
	}
}

func TestHoldsCrossPlatformExclusiveLifecycle(t *testing.T) {
	var s Core
	if s.Press(0, false) != 0 || s.Press(3, false) != 0 || s.Down(-1) {
		t.Fatal("invalid native code")
	}
	if s.Press(1, true) != 0 || s.Down(1) {
		t.Fatal("busy event created phantom hold")
	}
	a := s.Press(1, false)
	if a == 0 || !s.Armed() || !s.Down(1) {
		t.Fatal("owner was not armed")
	}
	if s.Press(2, false) != 0 || s.Down(2) {
		t.Fatal("second key should pass through")
	}
	if s.CanActivate(2, a) {
		t.Fatal("wrong activation owner")
	}
	if action := s.Release(1); action != ReleaseShort {
		t.Fatal("short click activated a task")
	}
	if s.Activate(1, a) {
		t.Fatal("late timer activated a short click")
	}
	a = s.Press(1, false)
	if a == 0 || !s.Activate(1, a) || s.Activate(1, a) {
		t.Fatal("long press must activate exactly once")
	}
	if action := s.Release(2); action != ReleaseIgnored {
		t.Fatal("phantom release")
	}
	if action := s.Release(1); action != ReleaseActive || s.Armed() {
		t.Fatal("owner release")
	}
	b := s.Press(1, false)
	if b == 0 || b == a || s.Activate(1, a) {
		t.Fatal("stale timer replay")
	}
	s.Abandon()
	if s.Armed() || !s.Down(1) {
		t.Fatal("abandoned press lost physical release")
	}
	if action := s.Release(1); action != ReleaseOther {
		t.Fatal("abandoned key created task")
	}
	c := s.Press(1, false)
	if c == 0 {
		t.Fatal("cannot start after abandon")
	}
	s.Cancel()
	if s.Down(1) || s.Armed() || s.Activate(1, c) {
		t.Fatal("cancel left live key")
	}
}

func TestOneCoreCancellationRevokesHoldAndWork(t *testing.T) {
	var core Core
	hold := core.Press(1, false)
	if hold == 0 || !core.Activate(1, hold) {
		t.Fatal("physical hold did not enter the shared core")
	}
	task := core.Start()
	ctx := core.ContextFor(task)
	core.Cancel()
	if core.Valid(task) || core.Armed() || core.Down(1) || core.Activate(1, hold) || ctx.Err() == nil {
		t.Fatal("one core cancel must revoke task, provider request and physical hold together")
	}
	if next := core.Start(); next == 0 || next == task || !core.Valid(next) {
		t.Fatal("core failed to admit the next task")
	}
}
