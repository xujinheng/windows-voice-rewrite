package input

import (
	"testing"
	"time"
)

func TestVoiceStartKeepsSideAndKeyboardWindowsApart(t *testing.T) {
	if HoldModeLabel(true, true) != "会议直译" || HoldModeLabel(false, true) != "会议意图" || HoldModeLabel(true, false) != "听写" || HoldModeLabel(false, false) != "改写" {
		t.Fatal("mode")
	}
	if RewriteHotkeyAction(true, false, true, false) != RewriteIgnore || RewriteHotkeyAction(false, true, true, false) != RewriteIgnore {
		t.Fatal("busy or paused does not finish")
	}
	if RewriteHotkeyAction(false, false, true, false) != RewriteFinishToggle || RewriteHotkeyAction(false, false, true, true) != RewriteIgnore {
		t.Fatal("recording")
	}
	if FinishRecording(false, true, true) != FinishToggle || FinishRecording(true, true, true) != FinishMeeting || FinishRecording(true, true, false) != FinishDictation || FinishRecording(true, false, false) != FinishSpokenRewrite {
		t.Fatal("finish")
	}
	if RewriteHotkeyAction(false, false, false, true) != RewriteStartHold || RewriteHotkeyAction(false, false, false, false) != RewritePrepareToggle {
		t.Fatal("start")
	}
}

func TestWindowsSideHoldDuration(t *testing.T) {
	if WindowsSideHold != 250*time.Millisecond {
		t.Fatalf("windows %s", WindowsSideHold)
	}
}

func TestHoldShortPressDoesNotActivate(t *testing.T) {
	var s Hold
	token, ok := s.Press()
	if !ok || !s.CanActivate(token) {
		t.Fatal("initial press must begin pending hold")
	}
	if held, active := s.Release(); !held || active {
		t.Fatalf("short release: held=%t active=%t", held, active)
	}
	if s.Activate(token) || s.CanActivate(token) {
		t.Fatal("expired timer activated a released button")
	}
}

func TestHoldLongPressStartsOnce(t *testing.T) {
	var s Hold
	token, ok := s.Press()
	if !ok || !s.Activate(token) {
		t.Fatal("hold must activate")
	}
	if s.CanActivate(token) || s.Activate(token) {
		t.Fatal("hold activated more than once")
	}
	if held, active := s.Release(); !held || !active {
		t.Fatalf("long release: held=%t active=%t", held, active)
	}
}

func TestHoldDuplicateAndStaleEvents(t *testing.T) {
	var s Hold
	if held, _ := s.Release(); held {
		t.Fatal("release without a press was handled")
	}
	first, _ := s.Press()
	if second, ok := s.Press(); ok || first != second {
		t.Fatal("duplicate down changed active press")
	}
	s.Release()
	next, ok := s.Press()
	if !ok || next == first || s.Activate(first) {
		t.Fatal("old timer activated new press")
	}
	if !s.Activate(next) {
		t.Fatal("current press did not activate")
	}
}

func TestReleaseDebouncedIgnoresOneSampleGap(t *testing.T) {
	polls := 0
	for _, pressed := range []bool{true, true, false, true, true, false} {
		if ReleaseDebounced(pressed, &polls) {
			t.Fatal("premature release")
		}
	}
	if !ReleaseDebounced(false, &polls) {
		t.Fatal("release not detected")
	}
}
