package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCleanTranscriptAllLanguages(t *testing.T) {
	cases := []struct{ in, out string }{
		{"language Chinese<asr_text>你好！", "你好！"},
		{"language English<asr_text>What's wrong with you?", "What's wrong with you?"},
		{"language English<asr_text>Hello. language Chinese<asr_text>你好。", "Hello. 你好。"},
		{"language German<asr_text>Guten Morgen.", "Guten Morgen."},
		{"language Japanese<asr_text>こんにちは", "こんにちは"},
		{"language Chinese,English<asr_text>Hi 你好", "Hi 你好"},
		{"language None<asr_text>", ""},
		{"Normal recognized text", "Normal recognized text"},
		{"language Chinese<asr_text>1. language English<asr_text>2. language Chinese<asr_text>3.", "1. 2. 3."},
	}
	for _, tc := range cases {
		if got := cleanTranscript(tc.in); got != tc.out {
			t.Errorf("input %q: got %q, want %q", tc.in, got, tc.out)
		}
	}
}

func TestProxyWithMultipartAndAllLanguages(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("path: %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Error("lost multipart")
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "pretend WAVE") {
			t.Error("lost WAV")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"language English<asr_text>Hello. language Chinese<asr_text>你好。","language":"en"}`)
	}))
	defer backend.Close()
	u, _ := url.Parse(backend.URL)
	ready := &atomic.Bool{}
	proxy := httptest.NewServer(makeHandler(u, backend.Client(), ready))
	defer proxy.Close()
	req, _ := http.NewRequest("POST", proxy.URL+"/v1/audio/transcriptions", strings.NewReader("pretend WAVE"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=abc")
	resp, err := proxy.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("not-ready status: %d", resp.StatusCode)
	}
	ready.Store(true)
	req, _ = http.NewRequest("POST", proxy.URL+"/v1/audio/transcriptions", strings.NewReader("pretend WAVE"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=abc")
	resp, err = proxy.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	got := map[string]string{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["text"] != "Hello. 你好。" {
		t.Fatalf("text: %#v", got)
	}
	if got["language"] != "en" {
		t.Fatalf("other field modified %#v", got)
	}
}

func TestConfigAndPaths(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BackendPort != 8080 || cfg.ListenAddress != "127.0.0.1:8081" {
		t.Fatal(cfg)
	}
	for _, name := range []string{"Qwen3-ASR-1.7B-Q8_0.gguf", "mmproj-Qwen3-ASR-1.7B-bf16.gguf"} {
		if err = os.WriteFile(filepath.Join(dir, name), []byte("fake"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	opts := cfg.toOptions(dir)
	if err = findModelPaths(dir, &opts); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(opts.mmprojPath, "bf16.gguf") {
		t.Fatal(opts.mmprojPath)
	}
	data, _ := os.ReadFile(cfgFile)
	if strings.Contains(string(data), "Users\\") {
		t.Fatal("private path in config")
	}
}

func TestLlamaArgs(t *testing.T) {
	args := llamaArgs(options{modelPath: "model.gguf", mmprojPath: "mmproj.gguf", gpuLayers: 99, contextSize: 8192, upstreamHost: "127.0.0.1", llamaPort: 8080})
	if strings.Join(args, " ") != "-m model.gguf --mmproj mmproj.gguf -ngl 99 -c 8192 -np 1 --host 127.0.0.1 --port 8080" {
		t.Fatalf("args mismatch %q", args)
	}
}

func newReadyProxy(t *testing.T, handler http.Handler) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	backend := httptest.NewServer(handler)
	t.Cleanup(backend.Close)
	u, _ := url.Parse(backend.URL)
	ready := &atomic.Bool{}
	ready.Store(true)
	proxy := httptest.NewServer(makeHandler(u, backend.Client(), ready))
	t.Cleanup(proxy.Close)
	return proxy, ready
}

func TestProxyAllowedRoutesAndReadiness(t *testing.T) {
	var calls atomic.Int32
	proxy, ready := newReadyProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-key" || r.URL.RawQuery != "test=1" {
			t.Error("authorization or query lost")
		}
		body, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodPost && string(body) != `{"content":"<asr_text>","parse_special":true}` {
			t.Errorf("request body changed: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"method":%q,"path":%q,"text":"language English<asr_text>literal"}`, r.Method, r.URL.Path)
	}))
	for _, route := range []struct{ method, path string }{
		{"POST", "/v1/chat/completions"}, {"POST", "/chat/completions"},
		{"POST", "/tokenize"}, {"POST", "/detokenize"},
		{"GET", "/v1/models"}, {"GET", "/models"},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			req, _ := http.NewRequest(route.method, proxy.URL+route.path+"?test=1", strings.NewReader(`{"content":"<asr_text>","parse_special":true}`))
			req.Header.Set("Authorization", "Bearer test-key")
			resp, err := proxy.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var body map[string]string
			if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 200 || body["method"] != route.method || body["path"] != route.path || body["text"] != "language English<asr_text>literal" {
				t.Fatalf("route or non-transcription body changed: status %d body %#v", resp.StatusCode, body)
			}
			ready.Store(false)
			defer ready.Store(true)
			req, _ = http.NewRequest(route.method, proxy.URL+route.path, nil)
			resp, err = proxy.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 503 {
				t.Fatalf("not-ready status = %d", resp.StatusCode)
			}
		})
	}
	if calls.Load() != 6 {
		t.Fatalf("upstream calls = %d", calls.Load())
	}
}

func TestProxyRejectsOtherPathsAndMethods(t *testing.T) {
	proxy, _ := newReadyProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unsupported route reached upstream")
	}))
	for _, route := range []struct{ method, path string }{
		{"GET", "/v1/chat/completions"}, {"GET", "/tokenize"}, {"POST", "/models"},
		{"DELETE", "/v1/models"}, {"POST", "/v1/tokenize"}, {"POST", "/slots/0"},
		{"POST", "/completion"}, {"POST", "/chat/completions/"}, {"POST", "/health"},
	} {
		req, _ := http.NewRequest(route.method, proxy.URL+route.path, nil)
		resp, err := proxy.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("%s %s = %d", route.method, route.path, resp.StatusCode)
		}
	}
}

func TestProxyStreamsEveryChunkBeforeCompletion(t *testing.T) {
	next := make(chan struct{}, 1)
	proxy, _ := newReadyProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Request-ID", "stream-test")
		for _, chunk := range []string{"data: language English<asr_text>first\n\n", "data: second\n\n"} {
			io.WriteString(w, chunk)
			w.(http.Flusher).Flush()
			select {
			case <-next:
			case <-r.Context().Done():
				return
			}
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", proxy.URL+"/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	resp, err := proxy.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Request-ID") != "stream-test" || resp.Header.Get("Content-Length") != "" {
		t.Fatalf("stream headers/status: %d %#v", resp.StatusCode, resp.Header)
	}
	reader := bufio.NewReader(resp.Body)
	for _, want := range []string{"data: language English<asr_text>first\n", "data: second\n"} {
		line, err := reader.ReadString('\n')
		if err != nil || line != want {
			t.Fatalf("stream chunk = %q, %v; want %q before upstream finishes", line, err, want)
		}
		if line, err = reader.ReadString('\n'); err != nil || line != "\n" {
			t.Fatalf("SSE delimiter = %q, %v", line, err)
		}
		next <- struct{}{}
	}
	rest, err := io.ReadAll(reader)
	if err != nil || string(rest) != "data: [DONE]\n\n" {
		t.Fatalf("stream ending = %q, %v", rest, err)
	}
}

func TestProxyClientCancellationStopsUpstream(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	proxy, _ := newReadyProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(stopped)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", proxy.URL+"/chat/completions", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := proxy.Client().Do(req)
		if err == nil {
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request never reached upstream")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not observe client cancellation")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not stop")
	}
}

func TestProxyTranscriptionPlainCleanupAndErrorPreservation(t *testing.T) {
	for _, status := range []int{200, 400} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			proxy, _ := newReadyProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(status)
				io.WriteString(w, "language English<asr_text>Hello")
			}))
			resp, err := proxy.Client().Post(proxy.URL+"/audio/transcriptions", "audio/wav", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			want := "language English<asr_text>Hello"
			if status == 200 {
				want = "Hello"
			}
			if resp.StatusCode != status || string(body) != want {
				t.Fatalf("response = %d %q", resp.StatusCode, body)
			}
		})
	}
}

func TestProxyRejectsOversizedBufferedResponse(t *testing.T) {
	proxy, _ := newReadyProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		chunk := strings.Repeat(" ", 1<<20)
		for i := 0; i < 128; i++ {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
		}
		io.WriteString(w, " ")
	}))
	resp, err := proxy.Client().Post(proxy.URL+"/v1/audio/transcriptions", "audio/wav", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("oversized response status = %d; truncated success must not be returned", resp.StatusCode)
	}
}

func TestProxyAbortsTruncatedUpstreamStream(t *testing.T) {
	proxy, _ := newReadyProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "1024")
		io.WriteString(w, "data: incomplete\n\n")
		w.(http.Flusher).Flush()
	}))
	resp, err := proxy.Client().Post(proxy.URL+"/chat/completions", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if string(body) != "data: incomplete\n\n" || err == nil {
		t.Fatalf("truncated stream must preserve bytes and report read failure: %q, %v", body, err)
	}
}


// Runs in the existing Windows OneClick CI job as well as on Linux and macOS.
// Cross-compilation proves Go source compatibility, not Metal hardware support.
func TestOneClickGoBuildsForBothMacArchitectures(t *testing.T) {
	for _, arch := range []string{"arm64", "amd64"} {
		t.Run(arch, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "oneclick-darwin-"+arch)
			cmd := exec.Command("go", "build", "-trimpath", "-o", file, ".")
			cmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH="+arch, "CGO_ENABLED=0")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("macOS %s Go cross-build: %v\n%s", arch, err, out)
			}
			if info, err := os.Stat(file); err != nil || info.Size() == 0 {
				t.Fatalf("macOS %s Go binary missing/empty: %v", arch, err)
			}
		})
	}
}
