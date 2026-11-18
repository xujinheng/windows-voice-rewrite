package input

import (
	"reflect"
	"sync"
	"testing"
)

func TestInputAdmissionAndRoute(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    Event
		want string
	}{
		{"dictation no AX", Event{Kind: "route", Action: "dictation"}, InputStart},
		{"paused", Event{Action: "dictation", Paused: true}, InputIgnore},
		{"busy", Event{Action: "dictation", Busy: true}, InputIgnore},
		{"starting", Event{Action: "dictation", Starting: true}, InputIgnore},
		{"processing", Event{Action: "dictation", Processing: true}, InputIgnore},
		{"meeting busy", Event{Action: "rewrite", MeetingBusy: true, TargetPresent: true}, InputIgnore},
		{"windows missing target", Event{Action: "dictation", RequireTarget: true}, InputMissingTarget},
		{"bound keyboard missing identity", Event{Action: "dictation", BoundDeviceID: "internal"}, InputIgnore},
		{"other keyboard", Event{Action: "dictation", BoundDeviceID: "internal", DeviceID: "external"}, InputIgnore},
		{"chosen keyboard", Event{Action: "dictation", BoundDeviceID: "internal", DeviceID: "internal"}, InputStart},
		{"dictation stop on toggle", Event{Action: "dictation", Recording: true}, InputStop},
		{"rewrite cannot stop dictation", Event{Action: "rewrite", Recording: true}, InputIgnore},
		{"rewrite stop its toggle", Event{Action: "rewrite", Recording: true, VoiceSelection: true}, InputStop},
		{"spoken no target", Event{Action: "rewrite"}, InputMissingTarget},
		{"spoken input", Event{Action: "rewrite", TargetPresent: true}, InputSpoken},
		{"typed empty", Event{Action: "rewrite", TypedInstruction: true, TargetPresent: true, Instruction: " \n"}, InputMissingInstruction},
		{"typed input", Event{Action: "rewrite", TypedInstruction: true, TargetPresent: true, Instruction: "改短"}, InputTyped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := InputRoute(tc.e).Decision; got != tc.want {
				t.Fatalf("got %s; want %s", got, tc.want)
			}
		})
	}
}

func TestFlowTraceMatchesNativeAdapters(t *testing.T) {
	cases := []struct {
		name   string
		events []Event
		want   []string
	}{
		{"keyboard release before ready", []Event{
			{Kind: "press", Source: "keyboard", Action: "dictation"},
			{Kind: "release", Source: "keyboard", Action: "dictation", Generation: 1, Starting: false},
			{Kind: "record_ready", Source: "keyboard", Action: "dictation", Generation: 1},
			{Kind: "record_ready", Source: "keyboard", Action: "dictation", Generation: 1},
		}, []string{"start", "wait", "stop", "ignore"}},
		{"keyboard normal", []Event{
			{Kind: "press", Source: "keyboard", Action: "rewrite"},
			{Kind: "record_ready", Source: "keyboard", Action: "rewrite", Generation: 1},
			{Kind: "release", Source: "keyboard", Action: "rewrite", Generation: 1, Recording: true},
			{Kind: "release", Source: "keyboard", Action: "rewrite", Generation: 1, Recording: true},
		}, []string{"start", "record", "stop", "ignore"}},
		{"side held and bounced", []Event{
			{Kind: "press", Source: "side", Action: "dictation", DeviceID: "Mouse4"},
			{Kind: "press", Source: "side", Action: "dictation", DeviceID: "Mouse4"},
			{Kind: "press", Source: "side", Action: "rewrite", DeviceID: "Mouse5"},
			{Kind: "record_ready", Source: "side", Action: "dictation", Generation: 1, DeviceID: "Mouse4"},
			{Kind: "release", Source: "side", Action: "dictation", Generation: 1, DeviceID: "Mouse4", Recording: true},
			{Kind: "release", Source: "side", Action: "dictation", Generation: 1, DeviceID: "Mouse4", Recording: true},
		}, []string{"start", "ignore", "ignore", "record", "stop", "ignore"}},
		{"stale epoch cannot stop fresh", []Event{
			{Kind: "press", Source: "keyboard", Action: "dictation"},
			{Kind: "cancel"},
			{Kind: "press", Source: "keyboard", Action: "rewrite"},
			{Kind: "record_ready", Source: "keyboard", Action: "rewrite", Generation: 3},
			{Kind: "release", Source: "keyboard", Action: "dictation", Generation: 1, Recording: true},
			{Kind: "release", Source: "keyboard", Action: "rewrite", Generation: 3, Recording: true},
		}, []string{"start", "clear", "start", "record", "ignore", "stop"}},
		{"focus change fences old ready", []Event{
			{Kind: "press", Source: "side", Action: "rewrite"},
			{Kind: "focus_changed"},
			{Kind: "record_ready", Source: "side", Action: "rewrite", Generation: 1},
		}, []string{"start", "clear", "ignore"}},
		{"no active on busy or pause", []Event{
			{Kind: "press", Source: "keyboard", Action: "dictation", Paused: true},
			{Kind: "press", Source: "keyboard", Action: "dictation", Busy: true},
			{Kind: "press", Source: "keyboard", Action: "dictation"},
		}, []string{"ignore", "ignore", "start"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var core Flow
			var trace []string
			for _, e := range tc.events {
				trace = append(trace, core.Step(e).Decision)
			}
			if !reflect.DeepEqual(trace, tc.want) {
				t.Fatalf("trace=%v; want %v", trace, tc.want)
			}
		})
	}
}

func TestFlowGenerationDoesNotReplayAfterTimeout(t *testing.T) {
	var f Flow
	start := f.Step(Event{Kind: "press", Source: "keyboard", Action: "dictation"})
	if start.Generation != 1 {
		t.Fatal(start)
	}
	if got := f.Step(Event{Kind: "timeout", Source: "keyboard", Action: "dictation", Generation: 1, Starting: true}); got.Decision != InputStop {
		t.Fatal(got)
	}
	next := f.Step(Event{Kind: "press", Source: "keyboard", Action: "dictation"})
	if next.Generation <= start.Generation {
		t.Fatal(next)
	}
	if got := f.Step(Event{Kind: "record_ready", Source: "keyboard", Action: "dictation", Generation: 1}); got.Decision != InputIgnore {
		t.Fatal(got)
	}
}

func TestFlowConcurrentReleaseAndCancel(t *testing.T) {
	for n := 0; n < 100; n++ {
		var f Flow
		start := f.Step(Event{Kind: "press", Source: "side", Action: "dictation"})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); f.Step(Event{Kind: "cancel"}) }()
		go func() {
			defer wg.Done()
			f.Step(Event{Kind: "release", Source: "side", Action: "dictation", Generation: start.Generation, Recording: true})
		}()
		wg.Wait()
		if got := f.Step(Event{Kind: "record_ready", Source: "side", Action: "dictation", Generation: start.Generation}); got.Decision != InputIgnore {
			t.Fatal(got)
		}
	}
}
