package voice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/ux"
)

func TestSharedVoiceTaskAdmissionAndRouting(t *testing.T) {
	var asr, llm atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			asr.Add(1)
			_, _ = io.WriteString(w, `{"text":"请写成英文"}`)
		case "/v1/chat/completions":
			llm.Add(1)
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\\"intent\\":\\"translate\\",\\"result\\":\\"Hello\\"}"}}]}`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL, cfg.LLMBaseURL = server.URL+"/v1", server.URL+"/v1"
	cfg.TextModel = "text-model"
	for _, n := range []int{0, 1, 3198, 3201, cfg.MaxRecordSeconds*32000 + 2} {
		_, err := Run(context.Background(), cfg, make([]byte, n), "", false, true, nil)
		var invalid *PCMError
		if !errors.As(err, &invalid) {
			t.Fatalf("accepted invalid PCM size %d: %v", n, err)
		}
		if n == 3198 && invalid.Error() != ux.DictationTooShort {
			t.Fatalf("short capture message %q", invalid.Error())
		}
	}
	if asr.Load() != 0 || llm.Load() != 0 {
		t.Fatal("invalid PCM contacted a provider")
	}
	pcm := make([]byte, 3200)
	for _, tc := range []struct {
		insert bool
		route string
	}{
		{true, "insert"}, {false, "preview"},
	} {
		got, err := Run(context.Background(), cfg, pcm, "", false, tc.insert, nil)
		if err != nil || got.Route != tc.route || got.Text != "请写成英文" {
			t.Fatalf("dictation: %+v %v", got, err)
		}
	}
	calls := 0
	got, err := Run(context.Background(), cfg, pcm, "你好", true, true, func(text string) {
		calls++
		if text != "请写成英文" { t.Errorf("instruction=%q", text) }
	})
	if err != nil || got.Text != "Hello" || got.Instruction != "请写成英文" ||
		got.Intent != "translate" || got.Route != "replace" || calls != 1 {
		t.Fatalf("rewrite: %+v err=%v calls=%d", got, err, calls)
	}
	if asr.Load() != 3 || llm.Load() != 1 {
		t.Fatalf("ASR=%d LLM=%d", asr.Load(), llm.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Run(ctx, cfg, pcm, "", false, true, nil)
	if !errors.Is(err, context.Canceled) || asr.Load() != 3 {
		t.Fatalf("canceled task contacted provider: %v", err)
	}
}
