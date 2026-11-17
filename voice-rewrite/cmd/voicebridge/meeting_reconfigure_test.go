package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	coreconfig "voicerewrite/internal/core/config"
	meetingcore "voicerewrite/internal/core/meeting"
)

func TestBridgeMeetingReconfigureFencesOldEpochAndKeepsConfirmedCaptions(t *testing.T) {
	cfg := coreconfig.Defaults()
	m, err := newBridgeMeeting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.stop()
	previous, epoch, _ := m.runtime.Snapshot()
	m.mu.Lock()
	m.captions = []meetingcore.Caption{
		{ID: 1, Source: "finished", Final: true},
		{ID: 2, Source: "old preview", Final: false},
	}
	m.mu.Unlock()
	next := cfg
	next.MeetingASRMode = coreconfig.MeetingASRMultipart
	next.MeetingBackground = "  role: engineer  "
	next.MeetingTerms = "  p99 = tail latency  "
	if err := m.reconfigure(next); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(previous.Err(), context.Canceled) {
		t.Fatal("old epoch not canceled")
	}
	if m.runtime.Active(epoch) {
		t.Fatal("old ASR epoch remains active")
	}
	if err := m.feed(make([]byte, 640), epoch); !errors.Is(err, errMeetingStaleEpoch) {
		t.Fatalf("old PCM accepted: %v", err)
	}
	snap := m.snapshot()
	if snap["profile_version"] != uint64(2) || snap["asr_mode"] != coreconfig.MeetingASRMultipart {
		t.Fatalf("reconfigure not published: %#v", snap)
	}
	if rows := snap["captions"].([]map[string]any); len(rows) != 1 || rows[0]["source"] != "finished" {
		t.Fatalf("old preview leaked or final lost: %#v", rows)
	}
	current := m.configSnapshot()
	if current.MeetingBackground != "role: engineer" || current.MeetingTerms != "p99 = tail latency" {
		t.Fatalf("profile not applied: %#v", current)
	}
	if err := m.reconfigure(next); err != nil {
		t.Fatal(err)
	}
	if m.snapshot()["profile_version"] != uint64(2) {
		t.Fatal("no-op reconfiguration reset epoch")
	}
}

func TestBridgeMeetingReconfigureInvalidProfileDoesNotChangeEpoch(t *testing.T) {
	m, err := newBridgeMeeting(coreconfig.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	defer m.stop()
	_, epoch, _ := m.runtime.Snapshot()
	next := m.configSnapshot()
	next.MeetingTerms = strings.Repeat("测", meetingcore.MaxTermsRunes+1)
	if err := m.reconfigure(next); err == nil {
		t.Fatal("overlong profile accepted")
	}
	next.MeetingTerms = ""
	next.MeetingASRMode = "invalid-mode"
	if err := m.reconfigure(next); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if _, activeEpoch, _ := m.runtime.Snapshot(); activeEpoch != epoch {
		t.Fatal("invalid config canceled active audio")
	}
}

func TestMeetingDemoIsOfflineAndNeverStartsCapture(t *testing.T) {
	s := &bridgeServer{}
	reply := s.meetingRequest(request{ID: "demo", Op: "meeting_demo"})
	if !reply.OK {
		t.Fatalf("demo failed: %#v", reply)
	}
	raw, err := json.Marshal(reply.Result)
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture["demo"] != true || fixture["session_id"] != "" {
		t.Fatalf("misrepresented demo: %#v", fixture)
	}
	captions := fixture["captions"].([]any)
	if len(captions) != 2 || captions[0].(map[string]any)["final"] != true {
		t.Fatal("missing prerecorded finalized demo captions")
	}
	replies := fixture["replies"].([]any)
	if len(replies) != 3 {
		t.Fatal("demo needs three offline suggestions")
	}
	if s.meeting != nil {
		t.Fatal("demo created a live meeting")
	}
}

func TestBridgeMeetingConfigureProtocolAppliesModeAndFencesPCM(t *testing.T) {
	h := startBridge(t, defaultOptions())
	defer h.close()
	cfg := configFor("http://127.0.0.1:8080")
	cfg["meeting_asr_mode"] = "multipart"
	h.send(map[string]any{"v": 1, "id": "profile-start", "op": "meeting_start", "config": cfg})
	start := h.receive()
	if !start.OK {
		t.Fatalf("cannot start Meeting fixture: %#v", start)
	}
	sid := object(t, start.Result)["session_id"].(string)
	cfg["meeting_asr_mode"] = "llama-prefix"
	cfg["meeting_background"] = "Project: Atlas"
	cfg["meeting_terms"] = "SLO = service level objective"
	h.send(map[string]any{"v": 1, "id": "configure", "op": "meeting_configure",
		"session_id": sid, "config": cfg})
	updated := h.receive()
	if !updated.OK {
		t.Fatalf("live configure failed: %#v", updated)
	}
	snap := object(t, updated.Result)
	if snap["profile_version"] != float64(2) || snap["epoch"] != float64(2) ||
		snap["asr_mode"] != "llama-prefix" {
		t.Fatalf("new mode/version not synchronized: %#v", snap)
	}
	h.send(map[string]any{"v": 1, "id": "old-packet", "op": "meeting_feed",
		"session_id": sid, "audio_epoch": 1,
		"pcm_base64": base64.StdEncoding.EncodeToString(make([]byte, 640))})
	late := h.receive()
	if !late.OK || object(t, late.Result)["stale_epoch"] != true {
		t.Fatalf("late pre-switch audio accepted: %#v", late)
	}
	cfg["meeting_asr_mode"] = "unknown"
	h.send(map[string]any{"v": 1, "id": "bad-configure", "op": "meeting_configure",
		"session_id": sid, "config": cfg})
	if bad := h.receive(); bad.OK || bad.Error == nil || bad.Error.Code != "invalid_config" {
		t.Fatalf("invalid mode accepted: %#v", bad)
	}
	h.send(map[string]any{"v": 1, "id": "after-invalid", "op": "meeting_snapshot", "session_id": sid})
	if current := h.receive(); !current.OK ||
		object(t, current.Result)["profile_version"] != float64(2) {
		t.Fatalf("invalid update changed running version: %#v", current)
	}
	h.send(map[string]any{"v": 1, "id": "profile-stop", "op": "meeting_stop", "session_id": sid})
	if stopped := h.receive(); !stopped.OK {
		t.Fatalf("unable to stop Meeting: %#v", stopped)
	}
}
