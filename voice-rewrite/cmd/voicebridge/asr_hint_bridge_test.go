package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Test the real JSONL helper route used by macOS, not just the shared Go
// multipart constructor. Off is backwards compatible and Meeting data stays
// private even with ASR hints enabled.
func TestVoicebridgeASRHintConfigRoundTripAndIsolation(t *testing.T) {
	var mu sync.Mutex
	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("unexpected endpoint %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		prompts = append(prompts, r.FormValue("prompt"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"PR 132 和 macOS"}`)
	}))
	defer server.Close()

	h := startBridge(t, defaultOptions())
	off := configFor(server.URL)
	off["meeting_terms"] = "MEETING_PRIVATE_TERM"
	h.send(requestForASR("off", off))
	if reply := h.receive(); reply.ID != "off" || !reply.OK {
		t.Fatalf("off config failed: %#v", reply)
	}

	on := configFor(server.URL)
	on["asr_hint_mode"] = "vocabulary-only"
	on["asr_vocabulary"] = "PR = P R\nmacOS = mac O S"
	on["meeting_terms"] = "MEETING_PRIVATE_TERM"
	on["system_prompt"] = "PRIVATE_SELECTED_TEXT"
	h.send(requestForASR("on", on))
	if reply := h.receive(); reply.ID != "on" || !reply.OK {
		t.Fatalf("on config failed: %#v", reply)
	}

	bad := configFor(server.URL)
	bad["asr_hint_mode"] = "unrecognized"
	h.send(requestForASR("bad", bad))
	if reply := h.receive(); reply.ID != "bad" || reply.OK || reply.Error == nil || reply.Error.Code != "invalid_config" {
		t.Fatalf("invalid mode was accepted: %#v", reply)
	}
	h.close()

	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("unexpected ASR call count %d", len(prompts))
	}
	if prompts[0] != "" {
		t.Fatalf("old helper behavior changed: %q", prompts[0])
	}
	if !strings.Contains(prompts[1], "PR (P R)") || !strings.Contains(prompts[1], "macOS (mac O S)") {
		t.Fatalf("hint was not propagated to ASR: %q", prompts[1])
	}
	if strings.Contains(prompts[1], "MEETING_PRIVATE_TERM") || strings.Contains(prompts[1], "PRIVATE_SELECTED_TEXT") {
		t.Fatal("private profile or LLM prompt leaked into ASR")
	}
}
