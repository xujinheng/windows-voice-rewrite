package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"voicerewrite/internal/core/session"
)

func TestInProcessRequestDeadlineCancelsASR(t *testing.T) {
	seen := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the multipart upload before waiting. An unread HTTP request
		// body can delay when the test server notices client disconnects.
		_, _ = io.Copy(io.Discard, r.Body)
		seen <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
			t.Error("ASR request did not propagate a deadline")
		}
	}))
	defer srv.Close()
	cfg, err := json.Marshal(configFor(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	reply := safeProcessRequest(ctx, request{
		ID: "native", Op: "transcribe", Config: cfg,
		NativePCM: make([]byte, 3200),
	})
	if reply.Error == nil || reply.Error.Code != "timeout" {
		t.Fatalf("in-process ASR did not stop at its deadline: %#v", reply)
	}
	select {
	case <-seen:
	default:
		t.Fatal("test did not reach the ASR network path")
	}
}

// Cancellation must reach an already-connected provider, not merely hide a
// stale completion in Swift. Both hosts call safeProcessRequest; macOS supplies
// the parent context through the native C ABI's session.Epoch.
func TestInProcessEpochCancellationReachesProvider(t *testing.T) {
	for _, tc := range []struct {
		name, operation, blockedPath string
		wantCalls                    int32
	}{
		{"asr", "transcribe", "/v1/audio/transcriptions", 1},
		{"rewrite_llm", "selected_voice_task", "/v1/chat/completions", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			disconnected := make(chan struct{}, 1)
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/v1/audio/transcriptions" && tc.blockedPath != r.URL.Path {
					_, _ = io.WriteString(w, `{"text":"请写成英文"}`)
					return
				}
				if r.URL.Path != tc.blockedPath {
					t.Errorf("unexpected provider request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_, _ = io.Copy(io.Discard, r.Body)
				entered <- struct{}{}
				select {
				case <-r.Context().Done():
					disconnected <- struct{}{}
				case <-time.After(4 * time.Second):
					t.Error("cancellation never disconnected the provider")
				}
			}))
			defer srv.Close()

			cfg := configFor(srv.URL)
			cfg["text_model"] = "text-model"
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			req := request{
				ID: "native", Op: tc.operation, Config: raw,
				SelectedText: "你好", NativePCM: make([]byte, 3200),
			}
			var epoch session.Epoch
			defer epoch.Shutdown()
			token := epoch.Start()
			done := make(chan response, 1)
			go func() { done <- safeProcessRequest(epoch.ContextFor(token), req) }()

			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not reach the provider before cancellation")
			}
			next := epoch.Start() // VREpochCancel invalidates the prior generation.
			select {
			case <-disconnected:
			case <-time.After(5 * time.Second):
				t.Fatal("provider did not observe the HTTP cancellation")
			}
			select {
			case reply := <-done:
				if reply.OK || reply.Error == nil || reply.Error.Code != "canceled" {
					t.Fatalf("canceled task returned a usable result: %#v", reply)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("canceled worker did not stop")
			}
			if epoch.Valid(token) || !epoch.Valid(next) {
				t.Fatal("stale generation remains eligible for native delivery")
			}
			// A delayed Swift worker must not contact the provider after cancel.
			reply := safeProcessRequest(epoch.ContextFor(token), req)
			if reply.OK || reply.Error == nil || reply.Error.Code != "canceled" {
				t.Fatalf("stale request was processed: %#v", reply)
			}
			if got := calls.Load(); got != tc.wantCalls {
				t.Fatalf("cancellation contacted %d provider endpoints, want %d", got, tc.wantCalls)
			}
		})
	}
}

func TestNativePCMAdmissionUsesSharedVoiceCore(t *testing.T) {
	cfg := configFor("http://127.0.0.1:1")
	cfg["max_record_seconds"] = 30
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, 1, 3198, 3201, 30*32000 + 2} {
		reply := safeProcessRequest(context.Background(), request{
			ID: "native", Op: "transcribe", Config: raw, NativePCM: make([]byte, n),
		})
		if reply.Error == nil || reply.Error.Code != "invalid_pcm" {
			t.Fatalf("accepted %d invalid PCM bytes: %#v", n, reply)
		}
	}
}

func configFor(url string) map[string]any {
	return map[string]any{
		"asr_base_url": url + "/v1", "llm_base_url": url + "/v1",
		"asr_api_key": "asr-private-key", "llm_api_key": "llm-private-key",
	}
}

func TestBridgeTranscribedStageOnlyOnRewrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			_, _ = io.WriteString(w, `{"text":"润色一下"}`)
		case "/v1/chat/completions":
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"intent\":\"edit\",\"result\":\"updated\"}"}}]}`)
		default:
			t.Errorf("unexpected provider endpoint: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	cfg := configFor(srv.URL)
	cfg["text_model"] = "test-model"
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	req := request{ID: "stage", Op: "selected_voice_task", Config: raw, NativePCM: make([]byte, 3200),
		SelectedText: "Original", AfterASR: func(text string) {
			if text != "润色一下" {
				t.Errorf("wrong transcript: %q", text)
			}
			calls++
		}}
	if reply := safeProcessRequest(context.Background(), req); !reply.OK || calls != 1 {
		t.Fatalf("rewrite stage not reported once: reply=%+v calls=%d", reply, calls)
	}
	req.Op = "transcribe"
	if reply := safeProcessRequest(context.Background(), req); !reply.OK || calls != 1 {
		t.Fatalf("dictation incorrectly reported rewrite stage: reply=%+v calls=%d", reply, calls)
	}
}
