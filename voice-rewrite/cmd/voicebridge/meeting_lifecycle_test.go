package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"voicerewrite/internal/core/audio"
	coreconfig "voicerewrite/internal/core/config"
	meetingcore "voicerewrite/internal/core/meeting"
)

func TestBridgeMeetingPauseResumeAndSessionIsolation(t *testing.T) {
	h := startBridge(t, defaultOptions())
	cfg := configFor("http://127.0.0.1:8080")
	cfg["meeting_asr_mode"] = "multipart"
	send := func(id, op, session string, fields map[string]any) response {
		t.Helper()
		req := map[string]any{"v": 1, "id": id, "op": op, "session_id": session}
		for key, value := range fields {
			req[key] = value
		}
		h.send(req)
		return h.receive()
	}
	start := send("initial-start", "meeting_start", "", map[string]any{"config": cfg})
	if !start.OK {
		t.Fatalf("start: %#v", start)
	}
	sid := object(t, start.Result)["session_id"].(string)
	pcm := base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes))
	initial := send("initial-snapshot", "meeting_snapshot", sid, nil)
	if !initial.OK || object(t, initial.Result)["capture_paused"] != false {
		t.Fatalf("unexpected initial pause state: %#v", initial)
	}
	initialEpoch := object(t, initial.Result)["epoch"].(float64)
	paused := send("pause", "meeting_pause", sid, nil)
	if !paused.OK {
		t.Fatalf("pause: %#v", paused)
	}
	pauseState := object(t, paused.Result)
	if pauseState["capture_paused"] != true || pauseState["level"] != float64(0) ||
		pauseState["epoch"].(float64) <= initialEpoch {
		t.Fatalf("pause did not invalidate active audio epoch: %#v", pauseState)
	}
	if got := send("paused-feed", "meeting_feed", sid, map[string]any{"pcm_base64": pcm, "audio_epoch": initialEpoch}); !got.OK || object(t, got.Result)["accepted"] != false || object(t, got.Result)["paused"] != true {
		t.Fatalf("paused PCM was not harmlessly dropped: %#v", got)
	}
	again := send("pause-again", "meeting_pause", sid, nil)
	if !again.OK || object(t, again.Result)["epoch"] != pauseState["epoch"] {
		t.Fatalf("repeated pause changed the work epoch: %#v", again)
	}
	resume := send("resume", "meeting_resume", sid, nil)
	resumed := object(t, resume.Result)
	if !resume.OK || resumed["capture_paused"] != false ||
		resumed["epoch"].(float64) <= pauseState["epoch"].(float64) {
		t.Fatalf("resume failed to create a new epoch: %#v", resume)
	}
	if got := send("old-epoch-feed", "meeting_feed", sid, map[string]any{
		"pcm_base64": pcm, "audio_epoch": initialEpoch,
	}); !got.OK || object(t, got.Result)["accepted"] != false || object(t, got.Result)["stale_epoch"] != true {
		t.Fatalf("old epoch PCM reached resumed segmentation: %#v", got)
	}
	if got := send("resumed-feed", "meeting_feed", sid, map[string]any{
		"pcm_base64": pcm, "audio_epoch": resumed["epoch"],
	}); !got.OK || object(t, got.Result)["accepted"] != true {
		t.Fatalf("resumed audio refused: %#v", got)
	}
	metrics, ok := resumed["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("no metrics for native Meeting HUD: %#v", resumed)
	}
	for _, key := range []string{"pending", "dropped", "asr_first_ms", "asr_done_ms",
		"translation_first_ms", "translation_done_ms"} {
		if _, exists := metrics[key]; !exists {
			t.Errorf("missing shared Meeting metric %q", key)
		}
	}
	if got := send("stop", "meeting_stop", sid, nil); !got.OK {
		t.Fatalf("stop: %#v", got)
	}
	if got := send("old-pause", "meeting_pause", sid, nil); got.OK || got.Error == nil || got.Error.Code != "stale_meeting" {
		t.Fatalf("stopped session was paused again: %#v", got)
	}
	next := send("next-start", "meeting_start", "", map[string]any{"config": cfg})
	if !next.OK {
		t.Fatalf("new session start: %#v", next)
	}
	nextID := object(t, next.Result)["session_id"].(string)
	if nextID == sid {
		t.Fatal("restart reused the previous session token")
	}
	if got := send("stale-resume", "meeting_resume", sid, nil); got.OK || got.Error == nil || got.Error.Code != "stale_meeting" {
		t.Fatalf("old resume touched new session: %#v", got)
	}
	fresh := send("next-snapshot", "meeting_snapshot", nextID, nil)
	if !fresh.OK || object(t, fresh.Result)["capture_paused"] != false ||
		object(t, fresh.Result)["epoch"] != float64(1) {
		t.Fatalf("new session inherited old lifecycle state: %#v", fresh)
	}
	if got := send("next-stop", "meeting_stop", nextID, nil); !got.OK {
		t.Fatalf("new session shutdown: %#v", got)
	}
	h.close()
}

func TestBridgeMeetingResumeRestartsSharedAudioStream(t *testing.T) {
	var asrCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			asrCalls.Add(1)
			_, _ = io.WriteString(w, `{"text":"audio after resume"}`)
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: "+`{"choices":[{"delta":{"content":"ZH\\t恢复识别\\n"}}]}`+"\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	cfg := configFor(srv.URL)
	cfg["meeting_asr_mode"] = "multipart"
	cfg["text_model"] = "chat-instruct"
	h.send(map[string]any{"v": 1, "id": "restart-start", "op": "meeting_start", "config": cfg})
	start := h.receive()
	if !start.OK {
		t.Fatalf("start: %#v", start)
	}
	sid := object(t, start.Result)["session_id"].(string)
	for _, action := range []string{"meeting_pause", "meeting_resume", "meeting_pause", "meeting_resume"} {
		h.send(map[string]any{"v": 1, "id": fmt.Sprintf("%s-%d", action, time.Now().UnixNano()),
			"op": action, "session_id": sid})
		if got := h.receive(); !got.OK {
			t.Fatalf("%s: %#v", action, got)
		}
	}
	pcm := make([]byte, 70*audio.FrameBytes)
	for i := 0; i < 20*audio.FrameBytes; i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], 2000)
	}
	for n := 0; n < 7; n++ {
		chunk := pcm[n*10*audio.FrameBytes : (n+1)*10*audio.FrameBytes]
		h.send(map[string]any{"v": 1, "id": fmt.Sprintf("restart-feed-%d", n),
			"op": "meeting_feed", "session_id": sid,
			"pcm_base64": base64.StdEncoding.EncodeToString(chunk)})
		if got := h.receive(); !got.OK {
			t.Fatalf("resumed packet %d rejected: %#v", n, got)
		}
	}
	found := false
	for n := 0; n < 150; n++ {
		h.send(map[string]any{"v": 1, "id": fmt.Sprintf("restart-poll-%d", n),
			"op": "meeting_snapshot", "session_id": sid})
		got := h.receive()
		if !got.OK {
			t.Fatalf("snapshot after resume: %#v", got)
		}
		rows, _ := object(t, got.Result)["captions"].([]any)
		for _, entry := range rows {
			row := object(t, entry)
			if row["source"] == "audio after resume" && row["final"] == true {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if !found || asrCalls.Load() == 0 {
		t.Fatal("resumed epoch did not process fresh audio through shared VAD/ASR")
	}
	h.send(map[string]any{"v": 1, "id": "restart-stop", "op": "meeting_stop", "session_id": sid})
	if got := h.receive(); !got.OK {
		t.Fatalf("shutdown: %#v", got)
	}
	h.close()
}

func TestBridgeMeetingRapidStopJoinsAudioConsumer(t *testing.T) {
	cfg := configFor("http://127.0.0.1:8080")
	// Exercise direct lifetime ownership without issuing requests to an ASR server.
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := coreconfig.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = coreconfig.Normalize(parsed)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 30; n++ {
		m, err := newBridgeMeeting(parsed)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			m.mu.Lock()
			m.captions = []meetingcore.Caption{
				{ID: 7, Source: "completed", Final: true},
				{ID: 8, Source: "outdated partial", Final: false},
			}
			m.mu.Unlock()
			_, epoch, _ := m.runtime.Snapshot()
			if err := m.setPaused(true); err != nil {
				t.Fatal(err)
			}
			remaining := m.snapshot()["captions"].([]map[string]any)
			if len(remaining) != 1 || remaining[0]["source"] != "completed" {
				t.Fatalf("pause retained stale provisional caption: %#v", remaining)
			}
			if err := m.setPaused(false); err != nil {
				t.Fatal(err)
			}
			if err := m.runReplies(context.Background(), 1, epoch); !errors.Is(err, context.Canceled) {
				t.Fatalf("automatic reply from stale epoch was admitted: %v", err)
			}
		}
		if err := m.setPaused(n%2 == 0); err != nil {
			t.Fatal(err)
		}
		m.stop()
		select {
		case <-m.audioDone:
		default:
			t.Fatal("meeting capture consumer survived stop")
		}
		if err := m.feed(make([]byte, audio.FrameBytes), 0); err == nil {
			t.Fatal("stopped meeting accepted packets")
		}
		m.stop()
	}
}
