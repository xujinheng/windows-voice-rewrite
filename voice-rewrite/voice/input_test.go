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
	s.Bind([]int{1}, []int{2})
	if s.Press(0, false) != 0 || s.Press(-1, false) != 0 || s.Down(-1) {
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
	core.Bind([]int{1}, []int{2})
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

func TestCorePhysicalRemapCannotReusePendingToken(t *testing.T) {
	var core Core
	core.Bind([]int{103}, []int{104})
	old := core.Press(103, false) // Quartz Mouse4
	if core.Release(103) != ReleaseShort || core.Down(103) {
		t.Fatal("old binding did not release")
	}
	core.Bind([]int{54}, []int{61})
	next := core.Press(54, false) // Quartz right Command
	if next == old || core.Activate(54, old) || !core.Activate(54, next) {
		t.Fatal("a stale timer from a remapped physical key activated the next input")
	}
}

func TestCoreRoleRemainsStableWhenShortcutsReload(t *testing.T) {
	var core Core
	core.Bind([]int{103}, []int{104})
	if core.Role(103) != 1 || core.Role(104) != 2 {
		t.Fatal("incorrect bound roles")
	}
	hold := core.Press(103, false)
	if hold == 0 || core.HeldRole(103) != 1 {
		t.Fatal("dictation mode was not captured at key-down")
	}
	core.Bind([]int{54}, []int{61})
	if core.Role(103) != 0 || core.HeldRole(103) != 1 {
		t.Fatal("hotkey reload silently changed in-flight task")
	}
	if !core.Activate(103, hold) || core.HeldRole(103) != 1 {
		t.Fatal("late activation lost original task role")
	}
	if core.Release(103) != ReleaseActive || core.HeldRole(103) != 0 {
		t.Fatal("released key retained a task role")
	}
	if core.Role(54) != 1 || core.Role(61) != 2 {
		t.Fatal("new bindings did not become active")
	}
}

func TestCoreRejectsUnboundPhysicalInput(t *testing.T) {
	var core Core
	if core.Press(1, false) != 0 {
		t.Fatal("an unconfigured client input must not begin a voice task")
	}
	core.Bind([]int{54}, []int{61})
	if core.Press(103, false) != 0 || core.Armed() {
		t.Fatal("an unbound native key was accepted")
	}
	if core.Press(54, false) == 0 {
		t.Fatal("configured shortcut was refused")
	}
}

func TestCoreWindowsMouseAndKeyboardShareOneInputOwner(t *testing.T) {
	var c Core
	c.Bind([]int{1, 256 + 11}, []int{2, 256 + 12})
	key := 256 + 11
	if c.Role(1) != 1 || c.Role(key) != 1 || c.Role(2) != 2 || c.Role(256+12) != 2 {
		t.Fatal("a host must bind both physical keyboard and mouse to the same intent")
	}
	token := c.Press(key, false)
	if token == 0 || !c.Activate(key, token) || c.HeldRole(key) != 1 {
		t.Fatal("keyboard hold bypassed the Go Core")
	}
	if c.Press(2, false) != 0 || c.Press(1, false) != 0 {
		t.Fatal("another physical input was accepted while keyboard owns the voice task")
	}
	if c.Release(key) != ReleaseActive || c.HeldRole(key) != 0 {
		t.Fatal("keyboard key release failed to clear Go Core ownership")
	}
}

func TestDifferentNativeEventsHaveIdenticalGoLifecycle(t *testing.T) {
	// The physical key/button numbers belong to the adapters; the business
	// events and cancellation outcomes must be identical.
	for _, native := range []struct {
		name            string
		dictation, edit int
	}{
		{"Windows mouse", 1, 2},
		{"Windows keyboard", 267, 268},
		{"macOS mouse", 103, 104},
		{"macOS keyboard", 54, 61},
	} {
		t.Run(native.name, func(t *testing.T) {
			var core Core
			core.Bind([]int{native.dictation}, []int{native.edit})
			if core.Role(native.dictation) != 1 || core.Role(native.edit) != 2 {
				t.Fatal("role drift")
			}
			token := core.Press(native.dictation, false)
			if token == 0 || !core.Activate(native.dictation, token) || core.Release(native.dictation) != ReleaseActive {
				t.Fatal("active hold drift")
			}
			short := core.Press(native.edit, false)
			if short == 0 || core.Release(native.edit) != ReleaseShort || core.Activate(native.edit, short) {
				t.Fatal("short-press drift")
			}
			generation := core.Start()
			ctx := core.ContextFor(generation)
			core.Cancel()
			if core.Valid(generation) || ctx.Err() == nil || core.Armed() {
				t.Fatal("cancellation drift")
			}
		})
	}
}
