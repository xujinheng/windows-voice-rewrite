package voice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/ux"
)

func TestRunSpokenTaskCancelsBeforeLLM(t *testing.T) {
	var llm atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/audio/transcriptions" {
			_, _ = io.WriteString(w, `{"text":"翻译成英文"}`)
			return
		}
		llm.Add(1)
		w.WriteHeader(500)
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL, cfg.LLMBaseURL = server.URL+"/v1", server.URL+"/v1"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callbacks := 0
	_, err := Run(ctx, cfg, make([]byte, 3200), "原文", true, func(instruction string) {
		callbacks++
		if instruction != "翻译成英文" {
			t.Errorf("unexpected transcript: %q", instruction)
		}
		cancel()
	})
	if !errors.Is(err, context.Canceled) || llm.Load() != 0 || callbacks != 1 {
		t.Fatalf("canceled between ASR and LLM: err=%v calls=%d callbacks=%d", err, llm.Load(), callbacks)
	}
}

func TestRunRejectsSelectionBeforeASR(t *testing.T) {
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL = "http://127.0.0.1:1/v1"
	for _, selected := range []string{"", "   ", strings.Repeat("字", cfg.MaxSelectedRunes+1)} {
		_, err := Run(context.Background(), cfg, make([]byte, 3200), selected, true, nil)
		if err == nil || (strings.TrimSpace(selected) == "" && err.Error() != ux.EmptySelection) {
			t.Fatalf("invalid selection reached ASR: selected_length=%d err=%v", len(selected), err)
		}
	}
}
