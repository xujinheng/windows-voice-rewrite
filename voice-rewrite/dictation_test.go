package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"voicerewrite/internal/core/speech"
)

func TestDictationTranscribesWithoutLLM(t *testing.T) {
	var asrCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		asrCalls++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		if r.FormValue("model") != "qwen3-asr" {
			t.Errorf("model=%s", r.FormValue("model"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"早上好，今天的会议开始。"}`))
	}))
	defer server.Close()
	cfg := defaults()
	cfg.ASRBaseURL = server.URL + "/v1"
	cfg.LLMBaseURL = "http://127.0.0.1:1/v1" // must not be called
	pcm := make([]byte, 6400)
	result, err := speech.Transcribe(context.Background(), cfg, pcm)
	if err != nil || result != "早上好，今天的会议开始。" || asrCalls != 1 {
		t.Fatalf("result=%q err=%v calls=%d", result, err, asrCalls)
	}
}
