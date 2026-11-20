package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"voicerewrite/internal/core/audio"
	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/jsonl"
	"voicerewrite/internal/core/session"
	"voicerewrite/internal/core/ux"
	"voicerewrite/internal/core/voice"
)

func TestSharedHostTimeouts(t *testing.T) {
	limits := defaultOptions()
	if limits.TranscribeTimeout != 220*time.Second || limits.SpokenSelectedTimeout != 240*time.Second {
		t.Fatalf("unexpected shared timeouts: %+v", limits)
	}
	// Mac native and JSONL dictation must use the same ASR budget as Windows,
	// independent of audio length. A slow provider must not time out on Mac only.
	for _, seconds := range []int{1, 11, 60} {
		pcm := make([]byte, seconds*audio.PCMBytesPerSecond)
		for _, req := range []request{
			{Op: "transcribe", NativePCM: pcm},
			{Op: "transcribe", PCMBase64: base64.StdEncoding.EncodeToString(pcm)},
		} {
			if got := limits.timeoutFor(req); got != 220*time.Second {
				t.Fatalf("%ds dictation budget %v, want 220s", seconds, got)
			}
		}
	}
	if got := limits.timeoutFor(request{Op: "selected_voice_task"}); got != 240*time.Second {
		t.Fatalf("selected-text budget %v, want 240s", got)
	}
	limits.TranscribeTimeout = 200 * time.Millisecond
	if got := limits.timeoutFor(request{Op: "transcribe"}); got != 200*time.Millisecond {
		t.Fatalf("custom test deadline was extended: %v", got)
	}
}

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
		NativePCM: make([]byte, 3200), CanInsert: true,
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
				ID: "native", Op: tc.operation, Config: raw, CanInsert: true,
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

func TestNativePCMUsesIdenticalGoValidation(t *testing.T) {
	cfg := coreconfig.Config{MaxRecordSeconds: 30}
	audio := make([]byte, 3200) // 100ms exactly, valid on both paths
	raw, nativeReason := decodeRequestPCM(request{NativePCM: audio}, cfg)
	encoded, jsonReason := decodeRequestPCM(request{PCMBase64: base64.StdEncoding.EncodeToString(audio)}, cfg)
	if nativeReason != "" || jsonReason != "" || !bytes.Equal(raw, encoded) {
		t.Fatalf("native and JSONL capture paths differ: native=%q JSON=%q", nativeReason, jsonReason)
	}
	for _, size := range []int{0, 1, 3201, 30*32000 + 2} {
		pcm := make([]byte, size)
		_, reason := decodeRequestPCM(request{NativePCM: pcm}, cfg)
		if reason == "" {
			t.Errorf("accepted invalid native PCM size %d", size)
		}

	}
	// The decoder owns framing and base64 only. Both production hosts use
	// voice.Run for the 100ms minimum and the final PCM admission decision.
	_, err := voice.Run(context.Background(), cfg, make([]byte, 3198), "", false, true, nil)
	var capture *voice.PCMError
	if !errors.As(err, &capture) || capture.Error() != ux.DictationTooShort {
		t.Fatalf("shared Go task accepted a short recording: %v", err)
	}
}

type bridgeHarness struct {
	t       *testing.T
	in      *io.PipeWriter
	out     <-chan response
	done    <-chan error
	writeMu sync.Mutex
}

func startBridge(t *testing.T, options bridgeOptions) *bridgeHarness {
	t.Helper()
	reader, writer := io.Pipe()
	resultReader, resultWriter := io.Pipe()
	responses := make(chan response, 64)
	done := make(chan error, 1)
	go func() {
		err := serveWithOptions(reader, resultWriter, options)
		_ = resultWriter.Close()
		done <- err
	}()
	go func() {
		defer close(responses)
		scanner := bufio.NewScanner(resultReader)
		scanner.Buffer(make([]byte, 1024), 2<<20)
		for scanner.Scan() {
			var reply response
			if json.Unmarshal(scanner.Bytes(), &reply) != nil {
				continue
			}
			responses <- reply
		}
		_ = resultReader.Close()
	}()
	return &bridgeHarness{t: t, in: writer, out: responses, done: done}
}

func (h *bridgeHarness) send(value any) {
	h.t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		h.t.Fatal(err)
	}
	h.raw(string(data) + "\n")
}

func (h *bridgeHarness) raw(data string) {
	h.t.Helper()
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if _, err := io.WriteString(h.in, data); err != nil {
		h.t.Fatal(err)
	}
}

func (h *bridgeHarness) receive() response {
	h.t.Helper()
	select {
	case reply, ok := <-h.out:
		if !ok {
			h.t.Fatal("helper output closed unexpectedly")
		}
		return reply
	case <-time.After(8 * time.Second):
		h.t.Fatal("timed out waiting for bridge reply")
	}
	return response{}
}

func (h *bridgeHarness) receiveIDs(ids ...string) map[string]response {
	h.t.Helper()
	found := make(map[string]response)
	for range ids {
		reply := h.receive()
		if _, duplicate := found[reply.ID]; duplicate {
			h.t.Fatalf("duplicate reply for %q", reply.ID)
		}
		found[reply.ID] = reply
	}
	for _, id := range ids {
		if _, ok := found[id]; !ok {
			h.t.Fatalf("missing reply for %q: %#v", id, found)
		}
	}
	return found
}

func (h *bridgeHarness) close() {
	h.t.Helper()
	_ = h.in.Close()
	select {
	case err := <-h.done:
		if err != nil {
			h.t.Fatalf("bridge failed: %v", err)
		}
	case <-time.After(8 * time.Second):
		h.t.Fatal("bridge did not exit after input EOF")
	}
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object, got %#v", value)
	}
	return result
}

func configFor(url string) map[string]any {
	return map[string]any{
		"asr_base_url": url + "/v1", "llm_base_url": url + "/v1",
		"asr_api_key": "asr-private-key", "llm_api_key": "llm-private-key",
	}
}

func requestForASR(id string, cfg map[string]any) map[string]any {
	return map[string]any{
		"v": 1, "id": id, "op": "transcribe", "config": cfg,
		"pcm_base64": base64.StdEncoding.EncodeToString(make([]byte, 3200)),
	}
}

func TestBridgeCoreHTTPRequestsAndRouting(t *testing.T) {
	var asrCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			asrCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer asr-private-key" {
				t.Error("missing ASR authorization")
			}
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				t.Errorf("invalid multipart: %v", err)
				w.WriteHeader(400)
				return
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer file.Close()
			wav := make([]byte, 44)
			if _, err := io.ReadFull(file, wav); err != nil || !bytes.Equal(wav[:4], []byte("RIFF")) ||
				!bytes.Equal(wav[8:12], []byte("WAVE")) {
				t.Error("ASR core did not send a WAV file")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"text":"language zh <asr_text> hello from core"}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	h := startBridge(t, defaultOptions())
	cfg := configFor(srv.URL)
	h.send(requestForASR("asr", cfg))
	reply := h.receive()
	if !reply.OK || object(t, reply.Result)["text"] != "hello from core" ||
		object(t, reply.Result)["route"] != "preview" {
		t.Fatalf("bad ASR response: %#v", reply)
	}
	h.close()
	if asrCalls.Load() != 1 {
		t.Fatalf("unexpected ASR calls: %d", asrCalls.Load())
	}
}

func TestBridgeInvalidConfigNeverContactsProvider(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	badConfig := configFor(srv.URL)
	badConfig["llm_base_url"] = "http://private:secret@localhost/v1"
	h.send(requestForASR("bad-asr", badConfig))
	h.send(map[string]any{
		"v": 1, "id": "bad-edit", "op": "selected_voice_task", "config": badConfig,
		"selected_text": "private selected text", "pcm_base64": base64.StdEncoding.EncodeToString(make([]byte, 3200)),
	})
	replies := h.receiveIDs("bad-asr", "bad-edit")
	for _, reply := range replies {
		if reply.OK || reply.Error == nil || reply.Error.Code != "invalid_config" {
			t.Fatalf("unexpected response: %#v", reply)
		}
	}
	h.close()
	if calls.Load() != 0 {
		t.Fatalf("invalid configuration caused %d HTTP calls", calls.Load())
	}
}

func TestBridgeTimeout(t *testing.T) {
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("HTTP request was not canceled")
		}
	}))
	defer srv.Close()
	options := defaultOptions()
	options.TranscribeTimeout = 150 * time.Millisecond
	h := startBridge(t, options)
	h.send(requestForASR("timeout", configFor(srv.URL)))
	if reply := h.receive(); reply.ID != "timeout" || reply.OK || reply.Error == nil || reply.Error.Code != "timeout" {
		t.Fatalf("expected timeout: %#v", reply)
	}
	select {
	case <-started:
	default:
		t.Error("timeout test did not reach HTTP server")
	}
	h.close()
}

func TestBridgePCMAndFrameLimits(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	cfg := configFor(srv.URL)
	for _, tc := range []struct{ id, audio string }{
		{"odd", base64.StdEncoding.EncodeToString([]byte{1})},
		{"invalid", "%%%"},
		{"empty", ""},
		{"duration", base64.StdEncoding.EncodeToString(make([]byte, maxPCMBytes+2))},
	} {
		h.send(map[string]any{
			"v": 1, "id": tc.id, "op": "transcribe", "config": cfg,
			"pcm_base64": tc.audio,
		})
		if reply := h.receive(); reply.ID != tc.id || reply.OK || reply.Error == nil || reply.Error.Code != "invalid_pcm" {
			t.Fatalf("invalid PCM accepted: %#v", reply)
		}
	}
	h.raw(strings.Repeat("x", jsonl.MaxRequestBytes) + "\n")
	if reply := h.receive(); reply.OK || reply.Error == nil || reply.Error.Code != "request_too_large" {
		t.Fatalf("oversized request not rejected: %#v", reply)
	}
	h.send(map[string]any{"v": 1, "id": "after-size-error", "op": "hello"})
	if reply := h.receive(); !reply.OK || reply.ID != "after-size-error" {
		t.Fatalf("reader failed to recover after oversized frame: %#v", reply)
	}
	h.close()
	if calls.Load() != 0 {
		t.Fatal("invalid audio contacted provider")
	}
}

func TestBridgeClassifiedFailureUsesSharedRecovery(t *testing.T) {
	const secret = "provider-body-must-not-appear"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, secret, http.StatusUnauthorized)
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	h.send(requestForASR("auth", configFor(srv.URL)))
	reply := h.receive()
	data, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(secret)) {
		t.Fatal("classified bridge error leaked the provider body")
	}
	if reply.OK || reply.Error == nil || reply.Error.Code != "asr_failed" || !strings.Contains(reply.Error.Message, "API 验证失败") {
		t.Fatalf("expected shared recovery text: %#v", reply)
	}
	h.close()
}

func TestBridgeEmptyTranscriptUsesSharedSentence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"   "}`))
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	h.send(requestForASR("empty", configFor(srv.URL)))
	reply := h.receive()
	if reply.OK || reply.Error == nil || reply.Error.Code != "asr_failed" || reply.Error.Message != ux.NoSpeech {
		t.Fatalf("empty transcript: %#v", reply)
	}
	h.close()
}

func TestBridgePrivateErrorsDoNotEchoInputs(t *testing.T) {
	privateKey := "secret-key-should-not-appear"
	privateText := "private-selection-should-not-appear"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, privateKey+" "+privateText, 500)
	}))
	defer srv.Close()
	cfg := configFor(srv.URL)
	cfg["llm_api_key"] = privateKey
	h := startBridge(t, defaultOptions())
	h.send(map[string]any{
		"v": 1, "id": "private", "op": "selected_voice_task", "config": cfg,
		"selected_text": privateText, "pcm_base64": base64.StdEncoding.EncodeToString(make([]byte, 3200)),
	})
	reply := h.receive()
	data, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(privateKey)) || bytes.Contains(data, []byte(privateText)) {
		t.Fatal("bridge error leaked credentials or selected text")
	}
	if reply.OK || reply.Error == nil || reply.Error.Code != "textedit_failed" {
		t.Fatalf("expected sanitized error: %#v", reply)
	}
	h.close()
}

func TestBridgeOneShotEOFWaitsForASRResponse(t *testing.T) {
	started := make(chan struct{}, 1)
	complete := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-complete
		_, _ = io.WriteString(w, `{"text":"one-shot transcription"}`)
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	h.send(requestForASR("eof", configFor(srv.URL)))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("ASR request never started")
	}
	// Swift closes its stdin pipe after sending exactly one request. The bridge
	// must wait for the provider instead of treating this normal EOF as cancel.
	_ = h.in.Close()
	close(complete)
	reply := h.receive()
	if !reply.OK || reply.Error != nil || object(t, reply.Result)["text"] != "one-shot transcription" {
		t.Fatalf("EOF canceled a valid dictation: %#v", reply)
	}
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EOF left helper blocked")
	}

	var sink closedWriter
	if err := serve(strings.NewReader(`{"v":1,"id":"hi","op":"hello"}`+"\n"), sink); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("expected broken pipe error, got %v", err)
	}
}

type closedWriter struct{}

func (closedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRetiredJSONLCancelOperationIsUnsupported(t *testing.T) {
	h := startBridge(t, defaultOptions())
	h.send(map[string]any{"v": 1, "id": "cancel", "op": "cancel", "target_id": "unknown"})
	reply := h.receive()
	if reply.OK || reply.Error == nil || reply.Error.Code != "unsupported_op" {
		t.Fatalf("JSONL diagnostic must not recreate process-local cancel state: %#v", reply)
	}
	h.close()
}

func TestBridgeSpokenSelectionUsesSharedASRAndTextedit(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			calls.Add(1)
			_, _ = io.WriteString(w, `{"text":"请写成英文"}`)
		case "/v1/chat/completions":
			calls.Add(1)
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"intent\":\"translate\",\"result\":\"Hello\"}"}}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	cfg := configFor(srv.URL)
	cfg["text_model"] = "text-model"
	h := startBridge(t, defaultOptions())
	h.send(map[string]any{
		"v": 1, "id": "spoken", "op": "selected_voice_task",
		"config": cfg, "selected_text": "你好",
		"pcm_base64": base64.StdEncoding.EncodeToString(make([]byte, 3200)),
	})
	reply := h.receive()
	if !reply.OK {
		t.Fatalf("spoken task failed: %#v", reply)
	}
	result := object(t, reply.Result)
	if result["instruction"] != "请写成英文" || result["text"] != "Hello" ||
		result["route"] != "replace" {
		t.Fatalf("unexpected spoken decision: %#v", result)
	}
	h.close()
	if calls.Load() != 2 {
		t.Fatalf("wanted one ASR and one LLM call, got %d", calls.Load())
	}
}

func TestBridgeSpokenSelectionRejectsBadPCMAndEmptySelection(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	cfg := configFor(srv.URL)
	for i, tc := range []struct {
		selected string
		audio    []byte
		want     string
		message  string
	}{
		{"text", make([]byte, 100), "invalid_pcm", ux.RewriteTooShort},
		{"", make([]byte, 3200), "invalid_request", ""},
		{"text", make([]byte, 3201), "invalid_pcm", ""},
	} {
		id := fmt.Sprintf("invalid-spoken-%d", i)
		h.send(map[string]any{
			"v": 1, "id": id, "op": "selected_voice_task",
			"config": cfg, "selected_text": tc.selected,
			"pcm_base64": base64.StdEncoding.EncodeToString(tc.audio),
		})
		reply := h.receive()
		if reply.OK || reply.Error == nil || reply.Error.Code != tc.want || (tc.message != "" && reply.Error.Message != tc.message) {
			t.Fatalf("unexpected response: %#v; want %s %s", reply, tc.want, tc.message)
		}
	}
	h.close()
	if calls.Load() != 0 {
		t.Fatal("rejected spoken requests contacted a provider")
	}
}
