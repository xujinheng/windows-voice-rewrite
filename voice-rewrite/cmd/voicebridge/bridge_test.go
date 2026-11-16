package main

import (
	"bufio"
	"bytes"
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

	"voicerewrite/internal/core/ux"
)

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
	var asrCalls, modelsCalls, completionCalls atomic.Int32
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
		case "/v1/models":
			modelsCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer llm-private-key" {
				t.Error("missing model discovery authorization")
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"example-instruct"}]}`)
		case "/v1/chat/completions":
			completionCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer llm-private-key" {
				t.Error("missing text completion authorization")
			}
			var payload struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Model != "example-instruct" {
				t.Error("textedit did not use discovered model")
			}
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"intent\":\"edit\",\"result\":\"Edited text\"}"}}]}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	h := startBridge(t, defaultOptions())
	cfg := configFor(srv.URL)
	h.send(requestForASR("asr", cfg))
	h.send(map[string]any{
		"v": 1, "id": "edit", "op": "selected_task", "config": cfg,
		"selected_text": "Original text", "instruction": "rewrite this more clearly",
	})
	replies := h.receiveIDs("asr", "edit")
	if !replies["asr"].OK || object(t, replies["asr"].Result)["text"] != "hello from core" {
		t.Fatalf("bad ASR response: %#v", replies["asr"])
	}
	edit := object(t, replies["edit"].Result)
	if !replies["edit"].OK || edit["intent"] != "edit" || edit["text"] != "Edited text" ||
		edit["preview_only"] != false || edit["may_attempt_replace"] != true {
		t.Fatalf("bad selected-task response: %#v", replies["edit"])
	}
	h.close()
	if asrCalls.Load() != 1 || modelsCalls.Load() != 1 || completionCalls.Load() != 1 {
		t.Fatalf("unexpected network calls: ASR=%d models=%d completions=%d",
			asrCalls.Load(), modelsCalls.Load(), completionCalls.Load())
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
		"v": 1, "id": "bad-edit", "op": "selected_task", "config": badConfig,
		"selected_text": "private selected text", "instruction": "rewrite",
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

func TestBridgeCancelStopsNetworkAndSuppressesSuccess(t *testing.T) {
	started := make(chan struct{}, 1)
	canceled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("HTTP request was not canceled")
		}
		canceled <- struct{}{}
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	h.send(requestForASR("slow", configFor(srv.URL)))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("ASR request never started")
	}
	h.send(map[string]any{"v": 1, "id": "stop", "op": "cancel", "target_id": "slow"})
	replies := h.receiveIDs("stop", "slow")
	if !replies["stop"].OK || object(t, replies["stop"].Result)["found"] != true {
		t.Fatalf("cancel request did not find active task: %#v", replies["stop"])
	}
	if replies["slow"].OK || replies["slow"].Error == nil || replies["slow"].Error.Code != "canceled" {
		t.Fatalf("canceled task reported success: %#v", replies["slow"])
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("network request did not observe context cancellation")
	}
	h.close()
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

func TestBridgeConcurrencyDuplicateIDAndBusy(t *testing.T) {
	started := make(chan struct{}, 2)
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
	options.MaxInFlight = 2
	h := startBridge(t, options)
	cfg := configFor(srv.URL)
	h.send(requestForASR("a", cfg))
	h.send(requestForASR("b", cfg))
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("expected two concurrent in-flight HTTP requests")
		}
	}
	h.send(requestForASR("a", cfg))
	h.send(requestForASR("c", cfg))
	h.send(map[string]any{"v": 1, "id": "stop-a", "op": "cancel", "target_id": "a"})
	h.send(map[string]any{"v": 1, "id": "stop-b", "op": "cancel", "target_id": "b"})
	// A duplicate ID produces its own error reply, even while the original
	// request is still pending. Capture both replies for "a".
	replies := make([]response, 0, 6)
	for range 6 {
		replies = append(replies, h.receive())
	}
	codes := map[string]int{}
	for _, reply := range replies {
		if reply.Error != nil {
			codes[reply.Error.Code]++
		}
		if strings.HasPrefix(reply.ID, "stop-") &&
			(!reply.OK || object(t, reply.Result)["found"] != true) {
			t.Fatalf("cancel did not find task: %#v", reply)
		}
	}
	for _, code := range []string{"duplicate_id", "busy", "canceled"} {
		want := 1
		if code == "canceled" {
			want = 2
		}
		if codes[code] != want {
			t.Fatalf("expected %d %s results, got %#v", want, code, replies)
		}
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
	h.raw(strings.Repeat("x", maxRequestBytes) + "\n")
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

func TestBridgeParsesSharedShortcut(t *testing.T) {
	h := startBridge(t, defaultOptions())
	h.send(map[string]any{"v": 1, "id": "shortcut", "op": "parse_shortcut", "shortcut": "ctrl+alt+r"})
	reply := h.receive()
	if !reply.OK || reply.Result == nil {
		t.Fatalf("shortcut: %#v", reply)
	}
	result := object(t, reply.Result)
	if result["normalized"] != "Ctrl+Alt+R" || result["key"] != "R" || result["ctrl"] != true || result["alt"] != true || result["mouse"] != false {
		t.Fatalf("binding: %#v", result)
	}
	h.send(map[string]any{"v": 1, "id": "bad-shortcut", "op": "parse_shortcut", "shortcut": "Shift+A"})
	rejected := h.receive()
	if rejected.OK || rejected.Error == nil || rejected.Error.Code != "invalid_shortcut" {
		t.Fatalf("invalid shortcut accepted: %#v", rejected)
	}
	h.close()
}

func TestBridgeUpdateRejectsUnknownVersionWithoutNetwork(t *testing.T) {
	h := startBridge(t, defaultOptions())
	h.send(map[string]any{"v": 1, "id": "update", "op": "check_update", "current_version": "not-a-version"})
	reply := h.receive()
	if reply.OK || reply.Error == nil || reply.Error.Code != "update_failed" || !strings.Contains(reply.Error.Message, "未识别的版本") {
		t.Fatalf("update check: %#v", reply)
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
		"v": 1, "id": "private", "op": "selected_task", "config": cfg,
		"selected_text": privateText, "instruction": "rewrite this",
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

func TestBridgeEOFAndOutputFailureReleaseTasks(t *testing.T) {
	started := make(chan struct{}, 1)
	canceled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("HTTP request was not canceled")
		}
		canceled <- struct{}{}
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	h.send(requestForASR("eof", configFor(srv.URL)))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("ASR request never started")
	}
	_ = h.in.Close()
	if reply := h.receive(); reply.OK || reply.Error == nil || reply.Error.Code != "canceled" {
		t.Fatalf("EOF did not cancel task: %#v", reply)
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("EOF did not cancel HTTP request")
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

func TestBridgeCancelMissing(t *testing.T) {
	h := startBridge(t, defaultOptions())
	h.send(map[string]any{"v": 1, "id": "cancel-missing", "op": "cancel", "target_id": "unknown"})
	reply := h.receive()
	if !reply.OK || object(t, reply.Result)["found"] != false {
		t.Fatalf("unexpected cancel response: %#v", reply)
	}
	h.close()
}

func TestBridgeServiceProbesUseSharedCoreWithoutUserContent(t *testing.T) {
	var asrCalls, modelCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			asrCalls.Add(1)
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer asr-private-key" {
				t.Error("ASR probe did not use configured provider authentication")
			}
			if err := r.ParseMultipartForm(2 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			audio, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer audio.Close()
			data, err := io.ReadAll(audio)
			if err != nil || len(data) != 32044 || !bytes.Equal(data[:min(4, len(data))], []byte("RIFF")) {
				t.Error("probe did not send one second of silent WAV via shared ASR request")
			} else {
				for _, sample := range data[44:] {
					if sample != 0 {
						t.Error("probe audio was not silent")
						break
					}
				}
			}
			w.WriteHeader(200)
		case "/v1/models":
			modelCalls.Add(1)
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer llm-private-key" {
				t.Error("LLM probe did not use configured provider authentication")
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"model-one"}]}`)
		default:
			t.Errorf("unexpected provider endpoint: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	cfg := configFor(srv.URL)
	cfg["asr_model"] = "whisper-1"
	cfg["text_model"] = "model-one"
	h.send(map[string]any{"v": 1, "id": "probe-a", "op": "probe_asr", "config": cfg})
	h.send(map[string]any{"v": 1, "id": "probe-l", "op": "probe_llm", "config": cfg})
	replies := h.receiveIDs("probe-a", "probe-l")
	for id, want := range map[string]string{"probe-a": "静音测试成功", "probe-l": "已在服务中发现"} {
		reply := replies[id]
		result := object(t, reply.Result)
		msg, _ := result["message"].(string)
		if !reply.OK || !strings.Contains(msg, want) {
			t.Fatalf("%s failed: %#v", id, reply)
		}
		encoded, _ := json.Marshal(reply)
		if bytes.Contains(encoded, []byte("private-key")) {
			t.Fatal("probe result leaked a credential")
		}
	}
	h.close()
	if asrCalls.Load() != 1 || modelCalls.Load() != 1 {
		t.Fatalf("expected one probe to each provider endpoint, got ASR %d LLM %d", asrCalls.Load(), modelCalls.Load())
	}
}

func TestBridgeProbeInvalidConfigNeverTouchesProvider(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	cfg := configFor(srv.URL)
	cfg["asr_base_url"] = "http://user:password@localhost/v1"
	h.send(map[string]any{"v": 1, "id": "probe-invalid", "op": "probe_asr", "config": cfg})
	reply := h.receive()
	if reply.OK || reply.Error == nil || reply.Error.Code != "invalid_config" {
		t.Fatalf("expected sanitized invalid_config: %#v", reply)
	}
	h.close()
	if calls.Load() != 0 {
		t.Fatal("invalid probe configuration reached provider")
	}
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
		result["may_attempt_replace"] != true || result["preview_only"] != false {
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
