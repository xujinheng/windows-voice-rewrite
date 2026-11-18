package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"voicerewrite/internal/core/audio"
	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/input"
	meetingcore "voicerewrite/internal/core/meeting"
	"voicerewrite/internal/core/speech"
	"voicerewrite/internal/core/textedit"
	"voicerewrite/internal/core/ux"
)

// Integration traces exercise real Go decisions and the in-process JSONL
// voicebridge used by macOS. An httptest provider replaces only external
// ASR/LLM networks. Native HID, AX and paste remain separate device gates.
func TestGo70IntegrationEffectTraces(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"dictation_result", traceDictation},
		{"selected_text_rewrite", traceSelectedRewrite},
		{"recording_180s_deadline", traceDeadline},
		{"release_finalizes_once", traceRelease},
		{"cancel_stale_completion", traceCancel},
		{"meeting_pause_resume", traceMeetingPauseResume},
		{"meeting_queue_full", traceMeetingQueueFull},
	} {
		t.Run(tc.name, tc.run)
	}
}

func traceConfig(t *testing.T, values map[string]any) coreconfig.Config {
	t.Helper()
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	config, err := coreconfig.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	config, err = coreconfig.Normalize(config)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func traceDictation(t *testing.T) {
	const transcript = "你好，跨平台听写。"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("wrong route: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		_, _ = io.WriteString(w, `{"text":"`+transcript+`"}`)
	}))
	defer srv.Close()
	cfg := configFor(srv.URL)
	pcm := make([]byte, audio.FrameBytes*6)
	// The Windows path calls this exact shared speech decision directly;
	// the macOS path reaches it through a real JSONL request and Go helper.
	direct, err := speech.Transcribe(context.Background(), traceConfig(t, cfg), pcm)
	if err != nil {
		t.Fatal(err)
	}
	h := startBridge(t, defaultOptions())
	h.send(map[string]any{"v": 1, "id": "dictation", "op": "transcribe", "config": cfg,
		"pcm_base64": base64.StdEncoding.EncodeToString(pcm), "can_insert": true})
	response := h.receive()
	if !response.OK {
		t.Fatalf("macOS helper: %#v", response)
	}
	bridge := object(t, response.Result)["text"]
	if route := object(t, response.Result)["route"]; route != string(ux.RouteDictationTranscript(direct, true)) {
		t.Fatalf("direct-Go/bridge dictation route divergence: %#v", route)
	}
	if direct != transcript || bridge != direct {
		t.Fatalf("direct-Go/bridge transcript divergence: direct=%q bridge=%#v", direct, bridge)
	}
	h.close()
}

func traceSelectedRewrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("wrong route: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"intent\":\"edit\",\"result\":\"Rewritten\"}"}}]}`)
	}))
	defer srv.Close()
	cfg := configFor(srv.URL)
	cfg["text_model"] = "test-model"
	const selection, instruction = "Original", "rewrite this more clearly"
	direct, err := textedit.ResolveSelectedTask(context.Background(), traceConfig(t, cfg), selection, instruction)
	if err != nil {
		t.Fatal(err)
	}
	directAction := textedit.RouteTask(direct, instruction, false)
	h := startBridge(t, defaultOptions())
	h.send(map[string]any{"v": 1, "id": "rewrite", "op": "selected_task", "config": cfg,
		"selected_text": selection, "instruction": instruction})
	res := h.receive()
	if !res.OK {
		t.Fatalf("macOS helper: %#v", res)
	}
	got := object(t, res.Result)
	expectedRoute := "preview"
	if directAction == textedit.Replace || directAction == textedit.TryReplace {
		expectedRoute = "replace"
	}
	if got["text"] != direct.Result || got["intent"] != direct.Intent ||
		got["route"] != expectedRoute {
		t.Fatalf("direct-Go/bridge write decision mismatch: action=%s direct=%#v bridge=%#v", directAction, direct, got)
	}
	h.close()
}

func traceDeadline(t *testing.T) {
	// An exactly 180s recording is still eligible for ASR; extra PCM is
	// rejected before network delivery, not silently truncated in the helper.
	cfg := traceConfig(t, configFor("http://127.0.0.1:12345"))
	if cfg.MaxRecordSeconds != 180 || input.HoldWatchLimit(cfg.MaxRecordSeconds) != 182*time.Second {
		t.Fatalf("shared deadline mismatch: %+v", cfg.MaxRecordSeconds)
	}
	pcm := make([]byte, 180*audio.PCMBytesPerSecond)
	req := request{PCMBase64: base64.StdEncoding.EncodeToString(pcm)}
	if _, reason := decodeRequestPCM(req, cfg); reason != "" {
		t.Fatalf("exact deadline rejected: %s", reason)
	}
	req.PCMBase64 = base64.StdEncoding.EncodeToString(append(pcm, 0, 0))
	if _, reason := decodeRequestPCM(req, cfg); reason != "invalid" {
		t.Fatalf("deadline overflow accepted: %q", reason)
	}
}

func traceRelease(t *testing.T) {
	// Execute the shared physical-button state machine. Late timer events
	// must not trigger a second stop after a release or a new press.
	var held input.Hold
	token, ok := held.Press()
	if !ok || !held.Activate(token) {
		t.Fatal("hold did not activate")
	}
	wasHeld, active := held.Release()
	if !wasHeld || !active {
		t.Fatalf("released active hold: %v %v", wasHeld, active)
	}
	if _, active = held.Release(); active || held.CanActivate(token) {
		t.Fatal("duplicate release or stale timer accepted")
	}
	if got := input.FinishRecording(true, true, false); got != input.FinishDictation {
		t.Fatalf("release routed to %v", got)
	}
	if got := input.FinishRecording(true, false, false); got != input.FinishSpokenRewrite {
		t.Fatalf("rewrite release routed to %v", got)
	}
}

func traceCancel(t *testing.T) {
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	h.send(requestForASR("old-epoch", configFor(srv.URL)))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider not reached")
	}
	h.send(map[string]any{"v": 1, "id": "cancel-old", "op": "cancel", "target_id": "old-epoch"})
	outcomes := h.receiveIDs("cancel-old", "old-epoch")
	if !outcomes["cancel-old"].OK || object(t, outcomes["cancel-old"].Result)["found"] != true ||
		outcomes["old-epoch"].Error == nil || outcomes["old-epoch"].Error.Code != "canceled" {
		t.Fatalf("late completion escaped cancellation: %#v", outcomes)
	}
	h.send(map[string]any{"v": 1, "id": "next-epoch", "op": "hello"})
	next := h.receive()
	if !next.OK || next.ID != "next-epoch" {
		t.Fatalf("stale/duplicate response: %#v", next)
	}
	h.close()
}

func traceMeetingPauseResume(t *testing.T) {
	direct := meetingcore.NewSessionRuntime()
	defer direct.Stop()
	_, first, _ := direct.Snapshot()
	if !direct.SetPaused(true) {
		t.Fatal("Go runtime refused pause")
	}
	_, paused, pauseFlag := direct.Snapshot()
	if !pauseFlag || direct.Active(first) {
		t.Fatal("old Go epoch accepted while paused")
	}
	if !direct.SetPaused(false) {
		t.Fatal("Go runtime refused resume")
	}
	_, resumed, resumeFlag := direct.Snapshot()
	if resumed <= paused || resumeFlag || direct.Active(first) == true {
		t.Fatal("Go runtime did not invalidate stale audio")
	}
	h := startBridge(t, defaultOptions())
	cfg := configFor("http://127.0.0.1:12345")
	cfg["meeting_asr_mode"] = "multipart"
	h.send(map[string]any{"v": 1, "id": "start", "op": "meeting_start", "config": cfg})
	start := h.receive()
	if !start.OK {
		t.Fatalf("meeting_start: %#v", start)
	}
	session := object(t, start.Result)["session_id"]
	h.send(map[string]any{"v": 1, "id": "pause", "op": "meeting_pause", "session_id": session})
	rp := h.receive()
	if !rp.OK || object(t, rp.Result)["capture_paused"] != true {
		t.Fatalf("meeting_pause: %#v", rp)
	}
	h.send(map[string]any{"v": 1, "id": "resume", "op": "meeting_resume", "session_id": session})
	rr := h.receive()
	if !rr.OK || object(t, rr.Result)["capture_paused"] != false {
		t.Fatalf("meeting_resume: %#v", rr)
	}
	epoch := object(t, rr.Result)["epoch"].(float64)
	if epoch != float64(resumed) {
		t.Fatalf("direct Go/bridge epoch mismatch: %v vs %d", epoch, resumed)
	}
	h.send(map[string]any{"v": 1, "id": "stale", "op": "meeting_feed", "session_id": session,
		"audio_epoch": first, "pcm_base64": base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes))})
	stale := h.receive()
	if !stale.OK || object(t, stale.Result)["stale_epoch"] != true {
		t.Fatalf("stale PCM accepted: %#v", stale)
	}
	h.send(map[string]any{"v": 1, "id": "stop", "op": "meeting_stop", "session_id": session})
	if stop := h.receive(); !stop.OK {
		t.Fatalf("meeting_stop: %#v", stop)
	}
	h.close()
}

func traceMeetingQueueFull(t *testing.T) {
	// A deterministic full bounded channel makes the production bridge feed
	// return busy without waiting on a real microphone or external model.
	r := meetingcore.NewSessionRuntime()
	defer r.Stop()
	_, epoch, _ := r.Snapshot()
	m := &bridgeMeeting{
		runtime: r, source: &meetingPacketSource{packets: make(chan []byte, 1)},
	}
	frame := make([]byte, audio.FrameBytes)
	if err := m.feed(frame, epoch); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if err := m.feed(frame, epoch); !errors.Is(err, errMeetingBusy) {
		t.Fatalf("full meeting queue did not fail closed: %v", err)
	}
	if m.dropped != 1 || len(m.source.packets) != 1 {
		t.Fatalf("backpressure bookkeeping: dropped=%d pending=%d", m.dropped, len(m.source.packets))
	}
	if err := m.setPaused(true); err != nil {
		t.Fatal(err)
	}
	if len(m.source.packets) != 0 {
		t.Fatal("paused meeting retained queued audio")
	}
	if err := m.feed(frame, epoch); !errors.Is(err, errMeetingPaused) {
		t.Fatalf("paused meeting fed audio: %v", err)
	}
	if err := m.setPaused(false); err != nil {
		t.Fatal(err)
	}
	_, fresh, _ := r.Snapshot()
	if err := m.feed(frame, epoch); !errors.Is(err, errMeetingStaleEpoch) {
		t.Fatalf("stale epoch after resume: %v", err)
	}
	if err := m.feed(frame, fresh); err != nil {
		t.Fatalf("fresh frame rejected: %v", err)
	}
}

// This is intentionally not a Windows HID or macOS AX/pasteboard acceptance
// claim: those require #73's real-device matrix after integration.
