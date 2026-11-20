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
	s.Apply(Command{Kind: CommandBind, Dictation: []int{1}, Rewrite: []int{2}})
	if s.Apply(Command{Kind: CommandPress, Code: 0, Busy: false}).Token != 0 || s.Apply(Command{Kind: CommandPress, Code: -1, Busy: false}).Token != 0 || s.Snapshot(-1, 0).Down {
		t.Fatal("invalid native code")
	}
	if s.Apply(Command{Kind: CommandPress, Code: 1, Busy: true}).Token != 0 || s.Snapshot(1, 0).Down {
		t.Fatal("busy event created phantom hold")
	}
	a := s.Apply(Command{Kind: CommandPress, Code: 1, Busy: false}).Token
	if a == 0 || !s.Snapshot(0, 0).Armed || !s.Snapshot(1, 0).Down {
		t.Fatal("owner was not armed")
	}
	if s.Apply(Command{Kind: CommandPress, Code: 2, Busy: false}).Token != 0 || s.Snapshot(2, 0).Down {
		t.Fatal("second key should pass through")
	}
	if s.Snapshot(2, a).CanActivate {
		t.Fatal("wrong activation owner")
	}
	if action := s.Apply(Command{Kind: CommandRelease, Code: 1}).Release; action != ReleaseShort {
		t.Fatal("short click activated a task")
	}
	if (s.Apply(Command{Kind: CommandActivate, Code: 1, Token: a}).Kind == EventActivated) {
		t.Fatal("late timer activated a short click")
	}
	a = s.Apply(Command{Kind: CommandPress, Code: 1, Busy: false}).Token
	if a == 0 || !(s.Apply(Command{Kind: CommandActivate, Code: 1, Token: a}).Kind == EventActivated) || (s.Apply(Command{Kind: CommandActivate, Code: 1, Token: a}).Kind == EventActivated) {
		t.Fatal("long press must activate exactly once")
	}
	if action := s.Apply(Command{Kind: CommandRelease, Code: 2}).Release; action != ReleaseIgnored {
		t.Fatal("phantom release")
	}
	if action := s.Apply(Command{Kind: CommandRelease, Code: 1}).Release; action != ReleaseActive || s.Snapshot(0, 0).Armed {
		t.Fatal("owner release")
	}
	b := s.Apply(Command{Kind: CommandPress, Code: 1, Busy: false}).Token
	if b == 0 || b == a || (s.Apply(Command{Kind: CommandActivate, Code: 1, Token: a}).Kind == EventActivated) {
		t.Fatal("stale timer replay")
	}
	s.Apply(Command{Kind: CommandAbandon})
	if s.Snapshot(0, 0).Armed || !s.Snapshot(1, 0).Down {
		t.Fatal("abandoned press lost physical release")
	}
	if action := s.Apply(Command{Kind: CommandRelease, Code: 1}).Release; action != ReleaseOther {
		t.Fatal("abandoned key created task")
	}
	c := s.Apply(Command{Kind: CommandPress, Code: 1, Busy: false}).Token
	if c == 0 {
		t.Fatal("cannot start after abandon")
	}
	s.Apply(Command{Kind: CommandCancel})
	if s.Snapshot(1, 0).Down || s.Snapshot(0, 0).Armed || (s.Apply(Command{Kind: CommandActivate, Code: 1, Token: c}).Kind == EventActivated) {
		t.Fatal("cancel left live key")
	}
}

func TestOneCoreCancellationRevokesHoldAndWork(t *testing.T) {
	var core Core
	core.Apply(Command{Kind: CommandBind, Dictation: []int{1}, Rewrite: []int{2}})
	hold := core.Apply(Command{Kind: CommandPress, Code: 1, Busy: false}).Token
	if hold == 0 || !(core.Apply(Command{Kind: CommandActivate, Code: 1, Token: hold}).Kind == EventActivated) {
		t.Fatal("physical hold did not enter the shared core")
	}
	task := core.Apply(Command{Kind: CommandStart}).Token
	ctx := core.ContextFor(task)
	core.Apply(Command{Kind: CommandCancel})
	if core.Snapshot(0, task).Valid || core.Snapshot(0, 0).Armed || core.Snapshot(1, 0).Down || (core.Apply(Command{Kind: CommandActivate, Code: 1, Token: hold}).Kind == EventActivated) || ctx.Err() == nil {
		t.Fatal("one core cancel must revoke task, provider request and physical hold together")
	}
	if next := core.Apply(Command{Kind: CommandStart}).Token; next == 0 || next == task || !core.Snapshot(0, next).Valid {
		t.Fatal("core failed to admit the next task")
	}
}

func TestCorePhysicalRemapCannotReusePendingToken(t *testing.T) {
	var core Core
	core.Apply(Command{Kind: CommandBind, Dictation: []int{103}, Rewrite: []int{104}})
	old := core.Apply(Command{Kind: CommandPress, Code: 103, Busy: false}).Token // Quartz Mouse4
	if core.Apply(Command{Kind: CommandRelease, Code: 103}).Release != ReleaseShort || core.Snapshot(103, 0).Down {
		t.Fatal("old binding did not release")
	}
	core.Apply(Command{Kind: CommandBind, Dictation: []int{54}, Rewrite: []int{61}})
	next := core.Apply(Command{Kind: CommandPress, Code: 54, Busy: false}).Token // Quartz right Command
	if next == old || (core.Apply(Command{Kind: CommandActivate, Code: 54, Token: old}).Kind == EventActivated) || !(core.Apply(Command{Kind: CommandActivate, Code: 54, Token: next}).Kind == EventActivated) {
		t.Fatal("a stale timer from a remapped physical key activated the next input")
	}
}

func TestCoreRoleRemainsStableWhenShortcutsReload(t *testing.T) {
	var core Core
	core.Apply(Command{Kind: CommandBind, Dictation: []int{103}, Rewrite: []int{104}})
	if core.Snapshot(103, 0).Role != 1 || core.Snapshot(104, 0).Role != 2 {
		t.Fatal("incorrect bound roles")
	}
	hold := core.Apply(Command{Kind: CommandPress, Code: 103, Busy: false}).Token
	if hold == 0 || core.Snapshot(103, 0).HeldRole != 1 {
		t.Fatal("dictation mode was not captured at key-down")
	}
	core.Apply(Command{Kind: CommandBind, Dictation: []int{54}, Rewrite: []int{61}})
	if core.Snapshot(103, 0).Role != 0 || core.Snapshot(103, 0).HeldRole != 1 {
		t.Fatal("hotkey reload silently changed in-flight task")
	}
	if !(core.Apply(Command{Kind: CommandActivate, Code: 103, Token: hold}).Kind == EventActivated) || core.Snapshot(103, 0).HeldRole != 1 {
		t.Fatal("late activation lost original task role")
	}
	if core.Apply(Command{Kind: CommandRelease, Code: 103}).Release != ReleaseActive || core.Snapshot(103, 0).HeldRole != 0 {
		t.Fatal("released key retained a task role")
	}
	if core.Snapshot(54, 0).Role != 1 || core.Snapshot(61, 0).Role != 2 {
		t.Fatal("new bindings did not become active")
	}
}

func TestCoreRejectsUnboundPhysicalInput(t *testing.T) {
	var core Core
	if core.Apply(Command{Kind: CommandPress, Code: 1, Busy: false}).Token != 0 {
		t.Fatal("an unconfigured client input must not begin a voice task")
	}
	core.Apply(Command{Kind: CommandBind, Dictation: []int{54}, Rewrite: []int{61}})
	if core.Apply(Command{Kind: CommandPress, Code: 103, Busy: false}).Token != 0 || core.Snapshot(0, 0).Armed {
		t.Fatal("an unbound native key was accepted")
	}
	if core.Apply(Command{Kind: CommandPress, Code: 54, Busy: false}).Token == 0 {
		t.Fatal("configured shortcut was refused")
	}
}

func TestCoreWindowsMouseAndKeyboardShareOneInputOwner(t *testing.T) {
	var c Core
	c.Apply(Command{Kind: CommandBind, Dictation: []int{1, 256 + 11}, Rewrite: []int{2, 256 + 12}})
	key := 256 + 11
	if c.Snapshot(1, 0).Role != 1 || c.Snapshot(key, 0).Role != 1 || c.Snapshot(2, 0).Role != 2 || c.Snapshot(256+12, 0).Role != 2 {
		t.Fatal("a host must bind both physical keyboard and mouse to the same intent")
	}
	token := c.Apply(Command{Kind: CommandPress, Code: key, Busy: false}).Token
	if token == 0 || !(c.Apply(Command{Kind: CommandActivate, Code: key, Token: token}).Kind == EventActivated) || c.Snapshot(key, 0).HeldRole != 1 {
		t.Fatal("keyboard hold bypassed the Go Core")
	}
	if c.Apply(Command{Kind: CommandPress, Code: 2, Busy: false}).Token != 0 || c.Apply(Command{Kind: CommandPress, Code: 1, Busy: false}).Token != 0 {
		t.Fatal("another physical input was accepted while keyboard owns the voice task")
	}
	if c.Apply(Command{Kind: CommandRelease, Code: key}).Release != ReleaseActive || c.Snapshot(key, 0).HeldRole != 0 {
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
			core.Apply(Command{Kind: CommandBind, Dictation: []int{native.dictation}, Rewrite: []int{native.edit}})
			if core.Snapshot(native.dictation, 0).Role != 1 || core.Snapshot(native.edit, 0).Role != 2 {
				t.Fatal("role drift")
			}
			token := core.Apply(Command{Kind: CommandPress, Code: native.dictation, Busy: false}).Token
			if token == 0 || !(core.Apply(Command{Kind: CommandActivate, Code: native.dictation, Token: token}).Kind == EventActivated) || core.Apply(Command{Kind: CommandRelease, Code: native.dictation}).Release != ReleaseActive {
				t.Fatal("active hold drift")
			}
			short := core.Apply(Command{Kind: CommandPress, Code: native.edit, Busy: false}).Token
			if short == 0 || core.Apply(Command{Kind: CommandRelease, Code: native.edit}).Release != ReleaseShort || (core.Apply(Command{Kind: CommandActivate, Code: native.edit, Token: short}).Kind == EventActivated) {
				t.Fatal("short-press drift")
			}
			generation := core.Apply(Command{Kind: CommandStart}).Token
			ctx := core.ContextFor(generation)
			core.Apply(Command{Kind: CommandCancel})
			if core.Snapshot(0, generation).Valid || ctx.Err() == nil || core.Snapshot(0, 0).Armed {
				t.Fatal("cancellation drift")
			}
		})
	}
}
