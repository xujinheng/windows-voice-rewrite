package input

import (
	"testing"
	"time"
)

func TestVoiceStartKeepsSideAndKeyboardWindowsApart(t *testing.T) {
	if HoldModeLabel(true, true) != "会议直译" || HoldModeLabel(false, true) != "会议意图" || HoldModeLabel(true, false) != "听写" || HoldModeLabel(false, false) != "改写" {
		t.Fatal("mode")
	}
}

func TestWindowsSideHoldDuration(t *testing.T) {
	if WindowsSideHold != 250*time.Millisecond {
		t.Fatalf("windows %s", WindowsSideHold)
	}
}

func TestHoldsCrossPlatformExclusiveLifecycle(t *testing.T) {
	s := NewHolds(105)
	if s.Press(0, false) != 0 || s.Press(105, false) != 0 || s.Down(-1) {
		t.Fatal("invalid native code")
	}
	if s.Press(54, true) != 0 || s.Down(54) {
		t.Fatal("busy event created phantom hold")
	}
	a := s.Press(54, false)
	if a == 0 || !s.Armed() || !s.Down(54) {
		t.Fatal("owner was not armed")
	}
	if s.Press(61, false) != 0 || s.Down(61) {
		t.Fatal("second key should pass through")
	}
	if s.CanActivate(61, a) {
		t.Fatal("wrong activation owner")
	}
	if held, active, primary := s.Release(54); !held || active || !primary {
		t.Fatal("short click activated a task")
	}
	if s.Activate(54, a) {
		t.Fatal("late timer activated a short click")
	}
	a = s.Press(54, false)
	if a == 0 || !s.Activate(54, a) || s.Activate(54, a) {
		t.Fatal("long press must activate exactly once")
	}
	if held, active, primary := s.Release(61); held || active || primary {
		t.Fatal("phantom release")
	}
	if held, active, primary := s.Release(54); !held || !active || !primary || s.Armed() {
		t.Fatal("owner release")
	}
	b := s.Press(54, false)
	if b == 0 || b == a || s.Activate(54, a) {
		t.Fatal("stale timer replay")
	}
	s.Abandon()
	if s.Armed() || !s.Down(54) {
		t.Fatal("abandoned press lost physical release")
	}
	if held, active, primary := s.Release(54); !held || active || primary {
		t.Fatal("abandoned key created task")
	}
	c := s.Press(54, false)
	if c == 0 {
		t.Fatal("cannot start after abandon")
	}
	s.Cancel()
	if s.Down(54) || s.Armed() || s.Activate(54, c) {
		t.Fatal("cancel left live key")
	}
}
