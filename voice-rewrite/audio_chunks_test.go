package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"voicerewrite/internal/core/audio"
	"voicerewrite/internal/core/speech"
)

func TestASRChunksMergeAndRespectCancel(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := calls.Add(1)
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		fmt.Fprintf(w, `{"text":"segment %d"}`, i)
	}))
	defer srv.Close()
	cfg := defaults()
	cfg.ASRBaseURL = srv.URL + "/v1"
	got, err := speech.Transcribe(context.Background(), cfg, make([]byte, 55*audio.PCMBytesPerSecond))
	if err != nil || !strings.Contains(got, "segment 1") || !strings.Contains(got, "segment 2") || calls.Load() < 2 {
		t.Fatalf("got=%q err=%v calls=%d", got, err, calls.Load())
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	before := calls.Load()
	if _, err = speech.Transcribe(canceled, cfg, make([]byte, 55*audio.PCMBytesPerSecond)); err == nil || calls.Load() != before {
		t.Fatalf("canceled task contacted server: %v", err)
	}
}
