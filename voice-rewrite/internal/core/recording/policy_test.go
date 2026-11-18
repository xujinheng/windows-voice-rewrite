package recording

import (
	"sync"
	"testing"
	"time"

	"voicerewrite/internal/core/config"
)

func TestLimitsPreserveSavedPreferences(t *testing.T) {
	cases := []struct{ input, seconds int }{
		{0, 180}, {-1, 180}, {181, 180}, {60, 60}, {1, 1}, {180, 180},
	}
	for _, tc := range cases {
		got := Limits(tc.input)
		if got.Seconds != tc.seconds || got.Watchdog() != time.Duration(tc.seconds+2)*time.Second {
			t.Errorf("Limits(%d) = %+v, want %ds and +2s", tc.input, got, tc.seconds)
		}
	}
	if Limits(0).Seconds != config.DefaultRecordSeconds {
		t.Fatal("Go config default drift")
	}
}

func TestDispatchNonMeeting(t *testing.T) {
	cases := map[Kind]Action{Dictation: SendASR, SpokenSelection: SendSpokenSelection, ToggleSelection: RunToggleSelection, "meeting": None}
	for kind, want := range cases {
		if got := Route(kind); got != want {
			t.Errorf("Route(%s)=%s want %s", kind, got, want)
		}
	}
}

func TestSessionExactlyOnceAndOldRelease(t *testing.T) {
	var c Controller
	if got := c.Begin("one", 1, Dictation, 60); got.Receipt != "accepted" || got.Window.Seconds != 60 {
		t.Fatalf("begin: %+v", got)
	}
	if got := c.Begin("one", 1, Dictation, 60); got.Receipt != "duplicate" {
		t.Fatalf("duplicate begin: %+v", got)
	}
	if got := c.Ready("one", 1, true); got.Receipt != "accepted" {
		t.Fatalf("ready: %+v", got)
	}
	if got := c.Commit("one", 1, Deadline); got.Receipt != "accepted" || got.Action != SendASR {
		t.Fatalf("deadline: %+v", got)
	}
	for _, cause := range []Cause{Released, Watchdog, Deadline} {
		if got := c.Commit("one", 1, cause); got.Action != None || got.Receipt != "duplicate" {
			t.Fatalf("duplicate %s: %+v", cause, got)
		}
	}
	if got := c.Begin("two", 2, SpokenSelection, 180); got.Receipt != "accepted" {
		t.Fatalf("new: %+v", got)
	}
	if got := c.Commit("one", 1, Released); got.Receipt != "stale" || got.Action != None {
		t.Fatalf("old release: %+v", got)
	}
	if got := c.Ready("two", 2, true); got.Receipt != "accepted" {
		t.Fatalf("ready 2 %+v", got)
	}
	if got := c.Commit("two", 2, Released); got.Action != SendSpokenSelection {
		t.Fatalf("rewrite: %+v", got)
	}
}

func TestCancelAndInvalidEvents(t *testing.T) {
	var c Controller
	if c.Begin("", 1, Dictation, 60).Receipt != "invalid" || c.Begin("id", 0, Dictation, 60).Receipt != "invalid" ||
		c.Begin("id", 1, "meeting", 60).Receipt != "invalid" {
		t.Fatal("invalid begin admitted")
	}
	c.Begin("first", 3, Dictation, 60)
	if got := c.Commit("first", 3, "not_a_cause"); got.Receipt != "invalid" {
		t.Fatalf("bad cause %+v", got)
	}
	if got := c.Cancel("first", 3); got.Receipt != "accepted" {
		t.Fatalf("cancel %+v", got)
	}
	if got := c.Commit("first", 3, Watchdog); got.Action != None || got.Receipt != "stale" {
		t.Fatalf("canceled emitted %+v", got)
	}
	c.Begin("new", 4, ToggleSelection, 30)
	if got := c.Cancel("first", 3); got.Receipt != "stale" {
		t.Fatalf("stale cancel %+v", got)
	}
	if got := c.Begin("older", 2, Dictation, 30); got.Receipt != "stale" {
		t.Fatalf("old begin %+v", got)
	}
	if got := c.Ready("new", 4, true); got.Receipt != "accepted" {
		t.Fatalf("toggle ready: %+v", got)
	}
	if got := c.Commit("new", 4, Released); got.Action != RunToggleSelection {
		t.Fatalf("toggle %+v", got)
	}
}

func TestConcurrentDeadlineReleaseCancel(t *testing.T) {
	for trial := 0; trial < 100; trial++ {
		var c Controller
		c.Begin("s", 1, Dictation, 180)
		c.Ready("s", 1, true)
		var wg sync.WaitGroup
		actions := make(chan Action, 32)
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				switch i % 3 {
				case 0:
					actions <- c.Commit("s", 1, Released).Action
				case 1:
					actions <- c.Commit("s", 1, Deadline).Action
				default:
					actions <- c.Commit("s", 1, Watchdog).Action
				}
			}(i)
		}
		wg.Wait()
		close(actions)
		count := 0
		for a := range actions {
			if a != None {
				count++
				if a != SendASR {
					t.Fatalf("unexpected %s", a)
				}
			}
		}
		if count != 1 {
			t.Fatalf("trial %d yielded %d submissions", trial, count)
		}
	}
}

func TestOldSessionCannotResumeAfterRestart(t *testing.T) {
	var c Controller
	c.Begin("a", 1, Dictation, 10)
	c.Cancel("a", 1)
	c.Begin("b", 2, Dictation, 30)
	c.Ready("b", 2, true)
	if got := c.Commit("a", 1, Watchdog); got.Action != None {
		t.Fatalf("stale watchdog: %+v", got)
	}
	if got := c.Commit("b", 2, Released); got.Action != SendASR {
		t.Fatalf("current session: %+v", got)
	}
}

func TestEarlyStopBeforeNativeReadyNeverSubmitsStalePCM(t *testing.T) {
	var c Controller
	if got := c.Begin("fast", 1, Dictation, 60); got.Receipt != "accepted" {
		t.Fatalf("begin: %+v", got)
	}
	if got := c.Commit("fast", 1, Released); got.Action != None || got.Receipt != "not_ready" {
		t.Fatalf("must wait for native start acknowledgement: %+v", got)
	}
	if got := c.Ready("fast", 1, true); got.Receipt != "accepted" {
		t.Fatalf("native ready: %+v", got)
	}
	if got := c.Commit("fast", 1, Released); got.Receipt != "accepted" || got.Action != SendASR {
		t.Fatalf("first authorized commit: %+v", got)
	}
	if got := c.Commit("fast", 1, Released); got.Action != None {
		t.Fatalf("duplicate fast release: %+v", got)
	}
}

func TestNativeMicrophoneStartFailureCannotSubmit(t *testing.T) {
	var c Controller
	c.Begin("denied", 1, Dictation, 180)
	if got := c.Ready("denied", 1, false); got.Receipt != "accepted" || got.Action != None {
		t.Fatalf("failed native recorder: %+v", got)
	}
	if got := c.Commit("denied", 1, Deadline); got.Receipt != "stale" || got.Action != None {
		t.Fatalf("failed capture must not send ASR: %+v", got)
	}
	c.Begin("recovered", 2, Dictation, 60)
	if got := c.Ready("recovered", 2, true); got.Receipt != "accepted" {
		t.Fatalf("recovered recorder: %+v", got)
	}
	if got := c.Commit("recovered", 2, Released); got.Action != SendASR {
		t.Fatalf("recovered capture: %+v", got)
	}
}
