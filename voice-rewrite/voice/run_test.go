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
	"time"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/config"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/ux"
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
	_, err := run(ctx, cfg, make([]byte, 3200), "原文", true, func(instruction string) {
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
		_, err := run(context.Background(), cfg, make([]byte, 3200), selected, true, nil)
		if err == nil || (strings.TrimSpace(selected) == "" && err.Error() != ux.EmptySelection) {
			t.Fatalf("invalid selection reached ASR: selected_length=%d err=%v", len(selected), err)
		}
	}
}

func TestPortablePCMAdmission(t *testing.T) {
	cfg := coreconfig.Defaults()
	cfg.MaxRecordSeconds = 30
	cfg.ASRBaseURL = "http://127.0.0.1:1/v1"
	for _, size := range []int{0, 1, 3198, 3201, 30*32000 + 2} {
		_, err := run(context.Background(), cfg, make([]byte, size), "", false, nil)
		var pcm *PCMError
		if !errors.As(err, &pcm) {
			t.Fatalf("PCM size %d was admitted: %v", size, err)
		}
	}
}

// Core.Execute handles cancellation for both native hosts.
func TestPortableEpochCancellationDisconnectsProvider(t *testing.T) {
	for _, rewrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "dictation", true: "rewrite"}[rewrite], func(t *testing.T) {
			entered := make(chan struct{}, 1)
			disconnected := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if rewrite && r.URL.Path == "/v1/audio/transcriptions" {
					_, _ = io.WriteString(w, `{"text":"rewrite this"}`)
					return
				}
				_, _ = io.Copy(io.Discard, r.Body)
				entered <- struct{}{}
				select {
				case <-r.Context().Done():
					disconnected <- struct{}{}
				case <-time.After(3 * time.Second):
					t.Error("provider never disconnected")
				}
			}))
			defer server.Close()
			cfg := coreconfig.Defaults()
			cfg.ASRBaseURL, cfg.LLMBaseURL = server.URL+"/v1", server.URL+"/v1"
			cfg.TextModel = "test-model"
			var epoch Core
			defer epoch.Apply(Command{Kind: CommandShutdown})
			token := epoch.Apply(Command{Kind: CommandStart}).Token
			done := make(chan error, 1)
			go func() {
				_, err := epoch.Execute(token, cfg, make([]byte, 3200), "selected text", rewrite, nil)
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("provider was never contacted")
			}
			epoch.Apply(Command{Kind: CommandStart}).Token
			select {
			case <-disconnected:
			case <-time.After(5 * time.Second):
				t.Fatal("provider did not observe cancellation")
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("portable task returned %v instead of cancellation", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("canceled task did not return")
			}
		})
	}
}

func TestCoreRetainsCompletedResultWithoutHostState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"text":"shared transcript"}`)
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL = server.URL + "/v1"
	var core Core
	token := core.Apply(Command{Kind: CommandStart}).Token
	out, err := core.Execute(token, cfg, make([]byte, 3200), "", false, nil)
	if err != nil || out.Text != "shared transcript" {
		t.Fatalf("execute result: %+v, %v", out, err)
	}
	entries := core.History.Snapshot()
	if len(entries) != 1 || entries[0].Text != out.Text || entries[0].Title != ux.DictationHistoryTitle {
		t.Fatalf("the sole Go Core must own result history: %+v", entries)
	}
}
