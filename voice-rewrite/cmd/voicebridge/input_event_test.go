package main

import (
	"testing"

	"voicerewrite/internal/core/input"
	"voicerewrite/internal/core/jsonl"
)

func TestInputEventsUseGoFlowAndFenceStaleReceipts(t *testing.T) {
	h := startBridge(t, defaultOptions())
	defer h.close()
	step := func(id string, e input.Event, want string, generation uint64) {
		t.Helper()
		h.send(map[string]any{"v": jsonl.Version, "id": id, "op": "input_event", "input": e})
		reply := h.receive()
		if !reply.OK {
			t.Fatalf("%s: %#v", id, reply)
		}
		got := object(t, reply.Result)
		if got["decision"] != want {
			t.Fatalf("%s: decision %v, expected %s", id, got["decision"], want)
		}
		if generation != 0 && got["generation"] != float64(generation) {
			t.Fatalf("%s: generation %v, expected %d", id, got["generation"], generation)
		}
	}
	step("press", input.Event{Kind: "press", Source: "keyboard", Action: "dictation"}, input.InputStart, 1)
	step("release", input.Event{Kind: "release", Source: "keyboard", Action: "dictation", Generation: 1, Starting: true}, input.InputWait, 1)
	step("ready", input.Event{Kind: "record_ready", Source: "keyboard", Action: "dictation", Generation: 1}, input.InputStop, 1)
	step("stale", input.Event{Kind: "record_ready", Source: "keyboard", Action: "dictation", Generation: 1}, input.InputIgnore, 0)
	step("route", input.Event{Kind: "route", Action: "rewrite", TargetPresent: true, TypedInstruction: true, Instruction: "polish"}, input.InputTyped, 0)
	step("no-target", input.Event{Kind: "route", Action: "rewrite", TargetPresent: false}, input.InputMissingTarget, 0)
}
