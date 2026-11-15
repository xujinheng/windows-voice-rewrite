package textedit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	coreconfig "voicerewrite/internal/core/config"
)

func TestResolveSpokenTaskSharesRealASRAndTextRouter(t *testing.T) {
	var asrCalls, llmCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			asrCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer asr-secret" {
				t.Error("ASR auth was not forwarded")
			}
			if err := r.ParseMultipartForm(2 << 20); err != nil {
				t.Errorf("bad ASR multipart: %v", err)
			}
			_, _ = w.Write([]byte(`{"text":"改成英文"}`))
		case "/v1/chat/completions":
			llmCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer llm-secret" {
				t.Error("LLM auth was not forwarded")
			}
			var req struct {
				Messages []struct { Content string `json:"content"` } `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("malformed LLM payload: %v", err)
			}
			if len(req.Messages) != 2 || !strings.Contains(req.Messages[1].Content, "原文") || !strings.Contains(req.Messages[1].Content, "改成英文") {
				t.Error("selected text or transcribed instruction was not forwarded")
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"intent\":\"translate\",\"result\":\"Original\"}"}}]}`))
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL = server.URL + "/v1"
	cfg.LLMBaseURL = server.URL + "/v1"
	cfg.ASRAPIKey, cfg.LLMAPIKey = "asr-secret", "llm-secret"
	cfg.TextModel = "text-model"
	var callbacks int
	result, spoken, err := ResolveSpokenTask(context.Background(), cfg, "原文", make([]byte, 3200), func(text string) {
		callbacks++
		if text != "改成英文" { t.Errorf("wrong spoken command: %q", text) }
	})
	if err != nil || spoken != "改成英文" || result.Intent != "translate" || result.Result != "Original" {
		t.Fatalf("result=%+v spoken=%q err=%v", result, spoken, err)
	}
	if got := RouteTask(result, spoken, false); got != TryReplace {
		t.Fatalf("guarded replacement denied by shared routing: %s", got)
	}
	if callbacks != 1 || asrCalls.Load() != 1 || llmCalls.Load() != 1 {
		t.Fatalf("unexpected calls: callback=%d asr=%d llm=%d", callbacks, asrCalls.Load(), llmCalls.Load())
	}
}

func TestResolveSpokenTaskRejectsInvalidSelectionAndAudioWithoutNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(200)
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL, cfg.LLMBaseURL = server.URL, server.URL
	for _, tc := range []struct {
		selected string
		audio []byte
	}{
		{"", make([]byte, 3200)},
		{"selected", make([]byte, 100)},
		{"selected", make([]byte, 3201)},
	} {
		if _, _, err := ResolveSpokenTask(context.Background(), cfg, tc.selected, tc.audio, nil); err == nil {
			t.Fatalf("accepted invalid input: selected=%q audio_bytes=%d", tc.selected, len(tc.audio))
		}
	}
	if requests.Load() != 0 { t.Fatal("invalid input contacted a provider") }
}

func TestResolveSpokenTaskCancelsBetweenASRAndLLM(t *testing.T) {
	var llmCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			_, _ = w.Write([]byte(`{"text":"翻译成英文"}`))
		default:
			llmCalls.Add(1)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL = server.URL + "/v1"
	cfg.LLMBaseURL = server.URL + "/v1"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _, err := ResolveSpokenTask(ctx, cfg, "原文", make([]byte, 3200), func(string) { cancel() })
	if err == nil || llmCalls.Load() != 0 { t.Fatalf("cancellation failed: err=%v llm=%d", err, llmCalls.Load()) }
}
