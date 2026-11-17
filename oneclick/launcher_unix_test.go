//go:build darwin || linux

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This is a deliberately fake HTTP backend. It proves transport and lifecycle
// behavior only; its responses are not evidence of ASR or GPU inference.
func TestOneClickFakeLlamaChild(t *testing.T) {
	if os.Getenv("ONECLICK_FAKE_LLAMA") != "1" {
		return
	}
	port := ""
	for i := 0; i+1 < len(os.Args); i++ {
		if os.Args[i] == "--port" {
			port = os.Args[i+1]
		}
	}
	if os.Getenv("ONECLICK_FAKE_EXIT") == "1" {
		os.Exit(17)
	}
	if port == "" {
		os.Exit(18)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"id":"mock-asr"}]}`)
	})
	mux.HandleFunc("/v1/audio/transcriptions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"text":"language Chinese<asr_text>测试完成"}`)
	})
	mux.HandleFunc("/tokenize", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"tokens":[1,2]}`)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"mock"}}]}`)
	})
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux}
	if err := server.ListenAndServe(); err != nil {
		os.Exit(19)
	}
}

func freeLocalPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func testOneClickExecutable(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "oneclick")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build launcher: %v\n%s", err, output)
	}
	return bin
}

func fakeLlamaScript(t *testing.T) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "llama-server")
	source := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--list-devices\" ]; then echo 'Metal: mock GPU'; echo 'CUDA0: mock GPU'; exit 0; fi\n" +
		"exec \"$ONECLICK_TEST_BINARY\" -test.run=^TestOneClickFakeLlamaChild$ -- \"$@\"\n"
	if err := os.WriteFile(name, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	return name
}

func fakeConfig(t *testing.T, script string, public, backend int) string {
	t.Helper()
	dir := t.TempDir()
	model := filepath.Join(dir, "model.gguf")
	mmproj := filepath.Join(dir, "audio.gguf")
	for _, path := range []string{model, mmproj} {
		if err := os.WriteFile(path, []byte("mock fixture, not a model"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config{
		LlamaServerPath: script,
		ModelPath: model,
		MMProjPath: mmproj,
		ListenAddress: fmt.Sprintf("127.0.0.1:%d", public),
		BackendHost: "127.0.0.1",
		BackendPort: backend,
		ContextSize: 2048,
		GPULayers: 0,
		AllowCPU: true,
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "config.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func waitHTTPReady(t *testing.T, endpoint string, cmd *exec.Cmd, logs *bytes.Buffer) {
	t.Helper()
	client := http.Client{Timeout: 400 * time.Millisecond}
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(endpoint + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("mock launcher never became ready (pid %d): %s", cmd.Process.Pid, logs.String())
}

func startMockLauncher(t *testing.T, bin, configPath string, extraEnv ...string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	testBin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-config", configPath)
	cmd.Env = append(os.Environ(), append([]string{
		"ONECLICK_TEST_BINARY=" + testBin, "ONECLICK_FAKE_LLAMA=1",
	}, extraEnv...)...)
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd, logs
}

func TestOneClickLauncherMockLifecycleAndAPIs(t *testing.T) {
	bin := testOneClickExecutable(t)
	exe := fakeLlamaScript(t)
	public, backend := freeLocalPort(t), freeLocalPort(t)
	if public == backend {
		backend = freeLocalPort(t)
	}
	cfg := fakeConfig(t, exe, public, backend)
	cmd, logs := startMockLauncher(t, bin, cfg)
	url := "http://127.0.0.1:" + strconv.Itoa(public)
	waitHTTPReady(t, url, cmd, logs)
	for _, test := range []struct {
		method, path, want string
	}{
		{"POST", "/v1/audio/transcriptions", "测试完成"},
		{"GET", "/v1/models", "mock-asr"},
		{"POST", "/v1/chat/completions", "mock"},
		{"POST", "/tokenize", "tokens"},
	} {
		req, _ := http.NewRequest(test.method, url+test.path, strings.NewReader("mock-input"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(body), test.want) {
			t.Fatalf("%s: status=%d body=%q", test.path, resp.StatusCode, body)
		}
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("SIGTERM should cleanly stop launcher: %v\n%s", err, logs.String())
	}
	time.Sleep(150 * time.Millisecond)
	for _, port := range []int{public, backend} {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Errorf("port %d remains in use after shutdown: %v", port, err)
		} else {
			listener.Close()
		}
	}
}

func TestOneClickLauncherDiagnosesMissingModelAndOccupiedPort(t *testing.T) {
	bin := testOneClickExecutable(t)
	exe := fakeLlamaScript(t)
	public, backend := freeLocalPort(t), freeLocalPort(t)
	cfgPath := fakeConfig(t, exe, public, backend)
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg.ModelPath); err != nil {
		t.Fatal(err)
	}
	b, err := exec.Command(bin, "-config", cfgPath).CombinedOutput()
	if err == nil || !bytes.Contains(b, []byte("model_path")) {
		t.Fatalf("missing model not diagnosed: %v %s", err, b)
	}
	if err := os.WriteFile(cfg.ModelPath, []byte("mock"), 0600); err != nil {
		t.Fatal(err)
	}
	occupied, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", backend))
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	b, err = exec.Command(bin, "-config", cfgPath).CombinedOutput()
	if err == nil || !bytes.Contains(b, []byte("already occupied")) {
		t.Fatalf("occupied backend not diagnosed: %v %s", err, b)
	}
}

func TestOneClickLauncherReportsUnexpectedChildExit(t *testing.T) {
	bin := testOneClickExecutable(t)
	exe := fakeLlamaScript(t)
	public, backend := freeLocalPort(t), freeLocalPort(t)
	cfg := fakeConfig(t, exe, public, backend)
	testBin, _ := os.Executable()
	cmd := exec.Command(bin, "-config", cfg)
	cmd.Env = append(os.Environ(), "ONECLICK_TEST_BINARY="+testBin, "ONECLICK_FAKE_LLAMA=1", "ONECLICK_FAKE_EXIT=1")
	out, err := cmd.CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte("exited before model was ready")) {
		t.Fatalf("unexpected child exit must be visible: %v %s", err, out)
	}
}

func TestOneClickAttachLeavesExternalLlamaRunning(t *testing.T) {
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"data":[{"id":"external"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer external.Close()
	backendURL, err := url.Parse(external.URL)
	if err != nil { t.Fatal(err) }
	backend, err := strconv.Atoi(backendURL.Port())
	if err != nil { t.Fatal(err) }
	public := freeLocalPort(t)
	if public == backend { public = freeLocalPort(t) }
	cfgPath := fakeConfig(t, fakeLlamaScript(t), public, backend)
	cfg, err := loadConfig(cfgPath)
	if err != nil { t.Fatal(err) }
	cfg.AttachToExistingServer = true
	cfg.LlamaServerPath, cfg.ModelPath, cfg.MMProjPath = "", "", ""
	b, err := json.Marshal(cfg)
	if err != nil { t.Fatal(err) }
	if err := os.WriteFile(cfgPath, b, 0600); err != nil { t.Fatal(err) }
	bin := testOneClickExecutable(t)
	cmd, logs := startMockLauncher(t, bin, cfgPath)
	frontend := fmt.Sprintf("http://127.0.0.1:%d", public)
	waitHTTPReady(t, frontend, cmd, logs)
	resp, err := http.Get(frontend + "/v1/models")
	if err != nil { t.Fatal(err) }
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Contains(data, []byte("external")) { t.Fatalf("attach proxy: %s", data) }
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil { t.Fatal(err) }
	if err := cmd.Wait(); err != nil { t.Fatal(err) }
	// An attached external process must not be killed by our launcher.
	resp, err = http.Get(external.URL + "/health")
	if err != nil { t.Fatalf("attached external backend was stopped: %v", err) }
	resp.Body.Close()
	if resp.StatusCode != 200 { t.Fatalf("attached backend status: %d", resp.StatusCode) }
}
