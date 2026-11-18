package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"voicerewrite/internal/core/jsonl"
)

func TestRecordingBridgeEffectsAndOldSession(t *testing.T) {
	var output bytes.Buffer
	server := &bridgeServer{
		active:  make(map[string]*bridgeTask),
		seen:    make(map[string]struct{}),
		output:  &output,
		options: defaultOptions(),
	}
	send := func(id, op, session, kind, cause string, generation uint64, seconds int) map[string]any {
		t.Helper()
		frame, err := json.Marshal(map[string]any{
			"v": jsonl.Version, "id": id, "op": op, "session_id": session,
			"generation": generation, "recording_kind": kind,
			"stop_cause": cause, "max_seconds": seconds, "native_ready": true,
		})
		if err != nil {
			t.Fatal(err)
		}
		output.Reset()
		server.dispatch(frame)
		var reply struct {
			OK     bool           `json:"ok"`
			Result map[string]any `json:"result"`
		}
		if err := json.Unmarshal(output.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if !reply.OK {
			t.Fatalf("bridge %s failed: %s", op, output.String())
		}
		return reply.Result
	}
	begin := send("one", "recording_begin", "s1", "dictation", "", 1, 60)
	if begin["receipt"] != "accepted" || begin["seconds"] != float64(60) || begin["watchdog_ms"] != float64(62000) {
		t.Fatalf("begin: %+v", begin)
	}
	ready := send("ready", "recording_ready", "s1", "", "", 1, 0)
	if ready["receipt"] != "accepted" {
		t.Fatalf("native ready: %+v", ready)
	}
	stop := send("stop", "recording_commit", "s1", "", "deadline", 1, 0)
	if stop["action"] != "send_asr" {
		t.Fatalf("deadline dropped audio: %+v", stop)
	}
	late := send("late", "recording_commit", "s1", "", "release", 1, 0)
	if late["action"] != "none" || late["receipt"] != "duplicate" {
		t.Fatalf("late release: %+v", late)
	}
	send("next", "recording_begin", "s2", "spoken_selection", "", 2, 180)
	_ = send("ready-two", "recording_ready", "s2", "", "", 2, 0)
	stale := send("stale", "recording_commit", "s1", "", "watchdog", 1, 0)
	if stale["action"] != "none" || stale["receipt"] != "stale" {
		t.Fatalf("old session: %+v", stale)
	}
	current := send("current", "recording_commit", "s2", "", "release", 2, 0)
	if current["action"] != "send_spoken_selection" {
		t.Fatalf("rewrite: %+v", current)
	}
	send("third", "recording_begin", "s3", "dictation", "", 3, 180)
	send("cancel", "recording_cancel", "s3", "", "", 3, 0)
	if got := send("canceled", "recording_commit", "s3", "", "deadline", 3, 0); got["action"] != "none" {
		t.Fatalf("canceled submitted: %+v", got)
	}
}
