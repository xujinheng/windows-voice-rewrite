package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"voicerewrite/internal/core/audio"
	meetingcore "voicerewrite/internal/core/meeting"
)

func TestBridgeMeetingLifecycleAndStalePackets(t *testing.T) {
	h := startBridge(t, defaultOptions())
	h.send(map[string]any{"v": 1, "id": "missing", "op": "meeting_start"})
	if got := h.receive(); got.OK || got.Error == nil || got.Error.Code != "invalid_config" {
		t.Fatalf("meeting accepted missing configuration: %#v", got)
	}
	cfg := configFor("http://127.0.0.1:8080")
	cfg["meeting_asr_mode"] = "multipart"
	h.send(map[string]any{"v": 1, "id": "start", "op": "meeting_start", "config": cfg})
	reply := h.receive()
	if !reply.OK {
		t.Fatalf("meeting start: %#v", reply)
	}
	session := object(t, reply.Result)["session_id"].(string)
	if len(session) != 32 {
		t.Fatalf("invalid session token: %q", session)
	}
	h.send(map[string]any{"v": 1, "id": "duplicate", "op": "meeting_start", "config": cfg})
	if got := h.receive(); got.OK || got.Error.Code != "meeting_active" {
		t.Fatalf("duplicate meeting start: %#v", got)
	}
	h.send(map[string]any{"v": 1, "id": "odd", "op": "meeting_feed", "session_id": session,
		"pcm_base64": base64.StdEncoding.EncodeToString([]byte{1, 2})})
	if got := h.receive(); got.OK || got.Error.Code != "invalid_pcm" {
		t.Fatalf("accepted partial PCM frame: %#v", got)
	}
	h.send(map[string]any{"v": 1, "id": "other", "op": "meeting_feed", "session_id": "old",
		"pcm_base64": base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes))})
	if got := h.receive(); got.OK || got.Error.Code != "stale_meeting" {
		t.Fatalf("old host session was accepted: %#v", got)
	}
	h.send(map[string]any{"v": 1, "id": "snapshot", "op": "meeting_snapshot", "session_id": session})
	got := h.receive()
	if !got.OK || object(t, got.Result)["session_id"] != session {
		t.Fatalf("meeting snapshot: %#v", got)
	}
	h.send(map[string]any{"v": 1, "id": "stop", "op": "meeting_stop", "session_id": session})
	if got := h.receive(); !got.OK || object(t, got.Result)["stopped"] != true {
		t.Fatalf("meeting did not stop: %#v", got)
	}
	h.send(map[string]any{"v": 1, "id": "late", "op": "meeting_snapshot", "session_id": session})
	if got := h.receive(); got.OK || got.Error.Code != "stale_meeting" {
		t.Fatalf("old snapshot unexpectedly accepted: %#v", got)
	}
	h.close()
}

func TestBridgeMeetingUsesSharedSegmentationAndASR(t *testing.T) {
	var asrCalls, completeASRCalls, translationCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			asrCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer asr-private-key" {
				t.Error("meeting ASR did not use configured ASR key")
			}
			if err := r.ParseMultipartForm(2 << 20); err != nil {
				// An in-flight preview can be superseded while the HTTP body is
				// uploading. Windows reports a truncated multipart request as
				// unexpected EOF even when the real final ASR succeeds later.
				// Require a complete request for any successful transcript.
				if err == io.ErrUnexpectedEOF || r.Context().Err() != nil {
					return
				}
				t.Errorf("invalid ASR WAV: %v", err)
				return
			}
			completeASRCalls.Add(1)
			_, _ = io.WriteString(w, `{"text":"hello from meeting"}`)
		case "/v1/chat/completions":
			translationCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer llm-private-key" {
				t.Error("meeting caption did not use configured LLM key")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: "+`{"choices":[{"delta":{"content":"ZH\t会议你好\n"}}]}`+"\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		default:
			t.Errorf("unexpected provider call %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	cfg := configFor(srv.URL)
	cfg["meeting_asr_mode"] = "multipart"
	cfg["text_model"] = "chat-instruct"
	h.send(map[string]any{"v": 1, "id": "start", "op": "meeting_start", "config": cfg})
	start := h.receive()
	if !start.OK {
		t.Fatalf("meeting start: %#v", start)
	}
	session := object(t, start.Result)["session_id"].(string)
	// Sustained voiced frames followed by silence must produce a final turn
	// through core/meeting.StreamAudio, not a parallel Swift VAD.
	pcm := make([]byte, 70*audio.FrameBytes)
	for i := 0; i < 20*audio.FrameBytes; i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], uint16(2000))
	}
	for n := 0; n < 7; n++ {
		chunk := pcm[n*10*audio.FrameBytes : (n+1)*10*audio.FrameBytes]
		h.send(map[string]any{"v": 1, "id": fmt.Sprintf("feed-%d", n), "op": "meeting_feed",
			"session_id": session, "pcm_base64": base64.StdEncoding.EncodeToString(chunk)})
		reply := h.receive()
		if !reply.OK {
			t.Fatalf("audio chunk %d: %#v", n, reply)
		}
	}
	deadline := time.Now().Add(6 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		h.send(map[string]any{"v": 1, "id": fmt.Sprintf("poll-%d", time.Now().UnixNano()),
			"op": "meeting_snapshot", "session_id": session})
		got := h.receive()
		if !got.OK {
			t.Fatalf("meeting snapshot: %#v", got)
		}
		captions, ok := object(t, got.Result)["captions"].([]any)
		if ok && len(captions) > 0 {
			row := object(t, captions[len(captions)-1])
			if row["source"] == "hello from meeting" && row["final"] == true {
				found = true
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	if !found || asrCalls.Load() == 0 || completeASRCalls.Load() == 0 {
		t.Fatal("shared core Meeting segmentation did not publish a final ASR caption")
	}
	if translationCalls.Load() == 0 {
		// Translation runs in its own bounded core queue; give it a short
		// opportunity to start after the final ASR callback.
		for n := 0; n < 50 && translationCalls.Load() == 0; n++ {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if translationCalls.Load() == 0 {
		t.Fatal("shared core Meeting caption translation was not requested")
	}
	h.send(map[string]any{"v": 1, "id": "stop", "op": "meeting_stop", "session_id": session})
	if !h.receive().OK {
		t.Fatal("meeting shutdown failed")
	}
	h.close()
}

func TestMeetingProtocolNeverEchoesPrivateConfiguration(t *testing.T) {
	h := startBridge(t, defaultOptions())
	secret := "secret-private-model-key"
	h.send(map[string]any{"v": 1, "id": "bad", "op": "meeting_start",
		"config": map[string]string{"asr_base_url": "http://user:" + secret + "@example.test/v1"}})
	reply := h.receive()
	if reply.OK || reply.Error == nil || strings.Contains(reply.Error.Message, secret) {
		t.Fatalf("invalid meeting configuration was echoed: %#v", reply)
	}
	h.close()
}

func TestBridgeMeetingViewUsesSharedHUDStateAndRejectsOtherButtons(t *testing.T) {
	h := startBridge(t, defaultOptions())
	cfg := configFor("http://127.0.0.1:8080")
	cfg["meeting_asr_mode"] = "multipart"
	h.send(map[string]any{"v": 1, "id": "start-view", "op": "meeting_start", "config": cfg})
	start := h.receive()
	if !start.OK {
		t.Fatalf("start: %#v", start)
	}
	sid := object(t, start.Result)["session_id"].(string)
	setView := func(id string, button int) response {
		t.Helper()
		h.send(map[string]any{
			"v": 1, "id": id, "op": "meeting_view",
			"session_id": sid, "button_id": button,
		})
		return h.receive()
	}
	captions := setView("focus", meetingcore.ButtonCaptions)
	if !captions.OK || object(t, captions.Result)["captions_only"] != true ||
		object(t, captions.Result)["expression_expanded"] != false {
		t.Fatalf("focus view: %#v", captions)
	}
	expanded := setView("expand", meetingcore.ButtonExpression)
	if !expanded.OK || object(t, expanded.Result)["captions_only"] != true ||
		object(t, expanded.Result)["expression_expanded"] != true {
		t.Fatalf("shared independent flags: %#v", expanded)
	}
	invalid := setView("unsafe-button", meetingcore.ButtonToggleCapture)
	if invalid.OK || invalid.Error == nil || invalid.Error.Code != "invalid_request" {
		t.Fatalf("arbitrary meeting control accepted: %#v", invalid)
	}
	captions = setView("all", meetingcore.ButtonCaptions)
	if !captions.OK || object(t, captions.Result)["captions_only"] != false ||
		object(t, captions.Result)["expression_expanded"] != true {
		t.Fatalf("return from focus lost expanded view: %#v", captions)
	}
	h.send(map[string]any{"v": 1, "id": "stop-view", "op": "meeting_stop", "session_id": sid})
	if got := h.receive(); !got.OK {
		t.Fatalf("stop: %#v", got)
	}
	h.send(map[string]any{"v": 1, "id": "new-view", "op": "meeting_start", "config": cfg})
	next := h.receive()
	if !next.OK {
		t.Fatalf("restart: %#v", next)
	}
	newID := object(t, next.Result)["session_id"].(string)
	h.send(map[string]any{"v": 1, "id": "snapshot-view", "op": "meeting_snapshot", "session_id": newID})
	view := h.receive()
	if !view.OK || object(t, view.Result)["captions_only"] != false ||
		object(t, view.Result)["expression_expanded"] != false {
		t.Fatalf("new session inherited old window flags: %#v", view)
	}
	h.send(map[string]any{"v": 1, "id": "done-view", "op": "meeting_stop", "session_id": newID})
	if got := h.receive(); !got.OK {
		t.Fatalf("shutdown: %#v", got)
	}
	h.close()
}
