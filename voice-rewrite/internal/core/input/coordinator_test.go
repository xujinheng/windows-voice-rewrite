package input

import (
	"sync"
	"testing"

	"voicerewrite/internal/core/voiceflow"
)

func TestCoordinatorExecutesActualVoiceflowEffects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		action string
		early  bool
	}{
		{"keyboard-normal", "keyboard", "dictation", false},
		{"side-normal", "side", "rewrite", false},
		{"keyboard-early-release", "keyboard", "rewrite", true},
		{"side-early-release", "side", "dictation", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var coordinator Coordinator
			start := coordinator.Step(Event{Kind: "press", Source: tc.source, Action: tc.action, TargetPresent: true})
			if start.Decision != InputStart || !hasAction(start.Actions, voiceflow.StartNativeCapture) {
				t.Fatalf("press not admitted by A engine: %+v", start)
			}
			release := func() Effect {
				return coordinator.Step(Event{Kind: "release", Source: tc.source, Action: tc.action,
					Generation: start.Generation, Starting: tc.early, Recording: !tc.early})
			}
			if tc.early {
				r := release()
				if r.Decision != InputWait || hasAction(r.Actions, voiceflow.StopNativeCapture) {
					t.Fatalf("premature stop on release-before-ready: %+v", r)
				}
			}
			ready := coordinator.Step(Event{Kind: "record_ready", Source: tc.source,
				Action: tc.action, Generation: start.Generation})
			if tc.early {
				if ready.Decision != InputStop || !hasAction(ready.Actions, voiceflow.StopNativeCapture) {
					t.Fatalf("queued release was not stopped by A engine: %+v", ready)
				}
			} else {
				if ready.Decision != "record" || !hasAction(ready.Actions, voiceflow.SetPhase) {
					t.Fatalf("mic ready did not advance A engine: %+v", ready)
				}
				r := release()
				if r.Decision != InputStop || !hasAction(r.Actions, voiceflow.StopNativeCapture) {
					t.Fatalf("release didn't reach A engine: %+v", r)
				}
			}
			stale := coordinator.Step(Event{Kind: "release", Source: tc.source,
				Action: tc.action, Generation: start.Generation, Recording: true})
			if stale.Decision != InputIgnore || hasAction(stale.Actions, voiceflow.StopNativeCapture) {
				t.Fatalf("duplicate release emitted native stop: %+v", stale)
			}
		})
	}
}

func TestCoordinatorOldReadyCannotStopNewTake(t *testing.T) {
	var c Coordinator
	first := c.Step(Event{Kind: "press", Source: "keyboard", Action: "dictation"})
	canceled := c.Step(Event{Kind: "cancel", Source: "keyboard"})
	if !hasAction(canceled.Actions, voiceflow.ClearTask) {
		t.Fatalf("A reducer did not cancel old generation: %+v", canceled)
	}
	next := c.Step(Event{Kind: "press", Source: "keyboard", Action: "rewrite", TargetPresent: true})
	if next.Generation <= first.Generation || !hasAction(next.Actions, voiceflow.StartNativeCapture) {
		t.Fatalf("new task not armed: %+v", next)
	}
	old := c.Step(Event{Kind: "record_ready", Source: "keyboard", Action: "dictation",
		Generation: first.Generation})
	if old.Decision != InputIgnore || len(old.Actions) != 0 {
		t.Fatalf("old mic receipt replayed: %+v", old)
	}
	ready := c.Step(Event{Kind: "record_ready", Source: "keyboard", Action: "rewrite",
		Generation: next.Generation})
	if ready.Decision != "record" {
		t.Fatalf("new mic couldn't become ready: %+v", ready)
	}
}

func TestCoordinatorConcurrentDuplicateEdgesRace(t *testing.T) {
	var c Coordinator
	start := c.Step(Event{Kind: "press", Source: "side", Action: "dictation"})
	c.Step(Event{Kind: "record_ready", Source: "side", Action: "dictation", Generation: start.Generation})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Step(Event{Kind: "release", Source: "side", Action: "dictation",
				Generation: start.Generation, Recording: true})
		}()
	}
	wg.Wait()
	got := c.Step(Event{Kind: "release", Source: "side", Action: "dictation",
		Generation: start.Generation, Recording: true})
	if got.Decision != InputIgnore || len(got.Actions) != 0 {
		t.Fatalf("repeat release executed effects: %+v", got)
	}
}
