// Qwen3-ASR OneClick starts llama.cpp and exposes ASR and continuation APIs.
// Windows x64 executable; Go standard library only; no Python or pip.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Qwen3-ASR can emit "language English<asr_text>...", "language Chinese<asr_text>...",
// and other language names, optionally multiple times in concatenated responses.
// The delimiter is treated as metadata only when immediately preceded by "language <name>".
var asrDelimiter = regexp.MustCompile(`(?i)language[ \t]+[a-z][a-z0-9 ,_+\-/]{0,79}[ \t]*<asr_text>`)

func cleanTranscript(s string) string {
	return strings.TrimSpace(asrDelimiter.ReplaceAllString(s, ""))
}

func cleanResponse(body []byte, contentType string) ([]byte, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err == nil {
		raw, exists := obj["text"]
		if !exists {
			return body, false
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return body, false
		}
		cleaned := cleanTranscript(s)
		if cleaned == s {
			return body, false
		}
		value, _ := json.Marshal(cleaned)
		obj["text"] = value
		updated, err := json.Marshal(obj)
		if err == nil {
			return updated, true
		}
		return body, false
	}
	if strings.Contains(strings.ToLower(contentType), "text/plain") {
		cleaned := cleanTranscript(string(body))
		if cleaned != string(body) {
			return []byte(cleaned), true
		}
	}
	return body, false
}

func makeHandler(upstream *url.URL, client *http.Client, ready *atomic.Bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if !ready.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintln(w, "llama.cpp is still starting")
				return
			}
			fmt.Fprintln(w, "Qwen3-ASR one-click OK; llama.cpp ready")
			return
		}
		transcription := false
		allowed := false
		switch r.URL.Path {
		case "/v1/audio/transcriptions", "/v1/audio/translations", "/audio/transcriptions", "/audio/translations":
			transcription = true
			allowed = r.Method == http.MethodPost
		case "/v1/chat/completions", "/chat/completions", "/tokenize", "/detokenize":
			allowed = r.Method == http.MethodPost
		case "/v1/models", "/models":
			allowed = r.Method == http.MethodGet
		}
		if !allowed {
			http.Error(w, "Unsupported API path or method", http.StatusNotFound)
			return
		}
		if !ready.Load() {
			http.Error(w, "llama.cpp model is still loading", http.StatusServiceUnavailable)
			return
		}
		target := *upstream
		target.Path = strings.TrimRight(upstream.Path, "/") + r.URL.Path
		target.RawQuery = r.URL.RawQuery
		req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		req.ContentLength = r.ContentLength
		req.Header = r.Header.Clone()
		req.Header.Set("Accept-Encoding", "identity")
		for _, header := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
			req.Header.Del(header)
		}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "Cannot reach llama.cpp at "+upstream.String()+": "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		streaming := strings.EqualFold(mediaType, "text/event-stream")
		var content []byte
		if !streaming {
			const maxResponseBytes = 128 << 20
			content, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
			if err != nil {
				http.Error(w, "llama.cpp response read failed: "+err.Error(), http.StatusBadGateway)
				return
			}
			if len(content) > maxResponseBytes {
				http.Error(w, "llama.cpp response exceeds 128 MiB", http.StatusBadGateway)
				return
			}
		}
		for key, values := range resp.Header {
			switch http.CanonicalHeaderKey(key) {
			case "Content-Length", "Content-Encoding", "Transfer-Encoding", "Connection", "Keep-Alive":
				continue
			default:
				for _, v := range values {
					w.Header().Add(key, v)
				}
			}
		}
		if streaming {
			w.WriteHeader(resp.StatusCode)
			controller := http.NewResponseController(w)
			if err := controller.Flush(); err != nil {
				return
			}
			buffer := make([]byte, 32<<10)
			for {
				n, readErr := resp.Body.Read(buffer)
				if n > 0 {
					if _, err := w.Write(buffer[:n]); err != nil {
						return
					}
					if err := controller.Flush(); err != nil {
						return
					}
				}
				if readErr != nil {
					if readErr != io.EOF && r.Context().Err() == nil {
						// Headers are committed; abort so a truncated stream is not a successful response.
						log.Printf("llama.cpp stream read failed: %v", readErr)
						panic(http.ErrAbortHandler)
					}
					return
				}
			}
		}
		if transcription && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var cleaned bool
			content, cleaned = cleanResponse(content, resp.Header.Get("Content-Type"))
			if cleaned {
				log.Printf("Cleaned Qwen3-ASR language metadata (%s)", r.URL.Path)
			}
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(content)
	})
}

type options struct {
	llamaPath    string
	modelPath    string
	mmprojPath   string
	listen       string
	upstreamHost string
	llamaPort    int
	contextSize  int
	gpuLayers    int
	attach       bool
	allowCPU     bool
}

type config struct {
	LlamaServerPath        string `json:"llama_server_path"`
	ModelPath              string `json:"model_path"`
	MMProjPath             string `json:"mmproj_path"`
	ListenAddress          string `json:"listen_address"`
	BackendHost            string `json:"backend_host"`
	BackendPort            int    `json:"backend_port"`
	ContextSize            int    `json:"context_size"`
	GPULayers              int    `json:"gpu_layers"`
	AttachToExistingServer bool   `json:"attach_to_existing_server"`
	AllowCPU               bool   `json:"allow_cpu"`
}

func defaultConfig() config {
	return config{
		LlamaServerPath: "llama-server.exe",
		ModelPath:       "Qwen3-ASR-1.7B-Q8_0.gguf",
		MMProjPath:      "mmproj-Qwen3-ASR-1.7B-bf16.gguf",
		ListenAddress:   "127.0.0.1:8080",
		BackendHost:     "127.0.0.1",
		BackendPort:     8082,
		ContextSize:     8192,
		GPULayers:       99,
	}
}

func loadConfig(path string) (config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		encoded, _ := json.MarshalIndent(cfg, "", "  ")
		if err = os.WriteFile(path, append(encoded, '\n'), 0600); err != nil {
			return cfg, fmt.Errorf("write config.json: %w", err)
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid config.json: %w", err)
	}
	if cfg.BackendPort < 1 || cfg.BackendPort > 65535 || cfg.ContextSize < 1 || cfg.GPULayers < 0 || cfg.ListenAddress == "" || cfg.BackendHost == "" {
		return cfg, fmt.Errorf("invalid listen_address/backend_host/backend_port/context_size/gpu_layers in config.json")
	}
	if cfg.LlamaServerPath == "" && !cfg.AttachToExistingServer {
		return cfg, fmt.Errorf("llama_server_path is empty")
	}
	return cfg, nil
}

func resolvePath(base, value string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(base, value)
}

func (c config) toOptions(base string) options {
	return options{
		llamaPath:    resolvePath(base, c.LlamaServerPath),
		modelPath:    resolvePath(base, c.ModelPath),
		mmprojPath:   resolvePath(base, c.MMProjPath),
		listen:       c.ListenAddress,
		upstreamHost: c.BackendHost,
		llamaPort:    c.BackendPort,
		contextSize:  c.ContextSize,
		gpuLayers:    c.GPULayers,
		attach:       c.AttachToExistingServer,
		allowCPU:     c.AllowCPU,
	}
}

func exeDir() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(path), nil
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p != "" {
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p
			}
		}
	}
	return ""
}

func findModelPaths(directory string, opts *options) error {
	if opts.modelPath == "" {
		opts.modelPath = firstExisting(filepath.Join(directory, "Qwen3-ASR-1.7B-Q8_0.gguf"))
	}
	if opts.modelPath == "" {
		return errors.New("main GGUF not configured: set model_path in config.json")
	}
	if info, err := os.Stat(opts.modelPath); err != nil || info.IsDir() {
		return fmt.Errorf("model_path is not a readable file: %q", opts.modelPath)
	}
	if opts.mmprojPath == "" {
		base := filepath.Dir(opts.modelPath)
		opts.mmprojPath = firstExisting(
			filepath.Join(base, "mmproj-Qwen3-ASR-1.7B-bf16.gguf"),
			filepath.Join(base, "mmproj-Qwen3-ASR-1.7B-Q8_0.gguf"),
		)
	}
	if opts.mmprojPath == "" {
		return errors.New("audio encoder not configured: set mmproj_path in config.json")
	}
	if info, err := os.Stat(opts.mmprojPath); err != nil || info.IsDir() {
		return fmt.Errorf("mmproj_path is not a readable file: %q", opts.mmprojPath)
	}
	return nil
}

func llamaArgs(o options) []string {
	return []string{"-m", o.modelPath, "--mmproj", o.mmprojPath,
		"-ngl", fmt.Sprint(o.gpuLayers), "-c", fmt.Sprint(o.contextSize),
		"-np", "1", "--host", o.upstreamHost, "--port", fmt.Sprint(o.llamaPort)}
}

func checkGPUs(path, cwd string, allowCPU bool) error {
	// A CPU-only launch is an explicit user choice. Do not require a GPU runtime.
	if allowCPU {
		return nil
	}
	cmd := exec.Command(path, "--list-devices")
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	fmt.Println("--- llama.cpp available devices ---")
	fmt.Print(string(output))
	fmt.Println("-----------------------------------")
	if err != nil {
		return fmt.Errorf("llama-server --list-devices failed: %w; check the selected binary and its dependencies", err)
	}
	switch runtime.GOOS {
	case "darwin":
		// macOS llama.cpp uses Metal, not CUDA. Neither the Swift launcher nor
		// this check claims that Metal inference works without a real model run.
		if !regexp.MustCompile(`(?i)\bmetal\b|\bmtl[0-9]+\b`).Match(output) {
			return errors.New("llama-server did not list a Metal device; select a Metal-enabled build or enable CPU fallback")
		}
	default:
		if !regexp.MustCompile(`(?i)\bCUDA[0-9]+\b`).Match(output) {
			return errors.New("llama-server did not list a CUDA device; check CUDA runtime libraries or enable CPU fallback")
		}
	}
	return nil
}

func waitForHealthy(ctx context.Context, port int, childExited <-chan error) error {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	target := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case err := <-childExited:
			return fmt.Errorf("llama-server.exe exited before model was ready: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func run(o options) error {
	directory, err := exeDir()
	if err != nil {
		return err
	}
	if o.llamaPath == "" {
		exe := "llama-server.exe"
		if runtime.GOOS != "windows" {
			exe = "llama-server"
		}
		o.llamaPath = filepath.Join(directory, exe)
	}
	if !o.attach {
		if info, err := os.Stat(o.llamaPath); err != nil || info.IsDir() {
			return fmt.Errorf("llama-server not found at %s; choose an executable llama-server and retry", o.llamaPath)
		}
	}
	if !o.attach {
		if err := findModelPaths(directory, &o); err != nil {
			return err
		}
	}
	// Bind the public-facing listener first so port collisions are obvious.
	listener, err := net.Listen("tcp", o.listen)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", o.listen, err)
	}
	defer listener.Close()
	addr := fmt.Sprintf("%s:%d", o.upstreamHost, o.llamaPort)
	backend, err := url.Parse("http://" + addr)
	if err != nil {
		return err
	}
	if !o.attach {
		if con, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
			con.Close()
			return fmt.Errorf("port %s is already occupied; close your old llama-server.exe first, or start with -attach to reuse it", addr)
		}
		if err := checkGPUs(o.llamaPath, filepath.Dir(o.llamaPath), o.allowCPU || o.gpuLayers == 0); err != nil {
			return err
		}
	}
	fmt.Printf("VoiceRewrite ASR URL: http://%s/v1\n", o.listen)
	fmt.Printf("Backend address: %s\n", backend)
	if !o.attach {
		fmt.Printf("Model: %s\nAudio encoder: %s\nGPU layers: %d; context: %d\n", o.modelPath, o.mmprojPath, o.gpuLayers, o.contextSize)
	}
	ready := &atomic.Bool{}
	client := &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{MaxIdleConnsPerHost: 8, ResponseHeaderTimeout: 9 * time.Minute}}
	srv := &http.Server{Handler: makeHandler(backend, client, ready), ReadHeaderTimeout: 20 * time.Second, IdleTimeout: 2 * time.Minute, WriteTimeout: 10 * time.Minute}
	errs := make(chan error, 1)
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	defer srv.Shutdown(context.Background())
	// Lifetime is tied to this EXE: closing the window / Ctrl+C stops the child.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	childExited := make(chan error, 1)
	var child *exec.Cmd
	var releaseJob func()
	if !o.attach {
		child = exec.Command(o.llamaPath, llamaArgs(o)...)
		child.Dir = filepath.Dir(o.llamaPath)
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		child.Stdin = os.Stdin
		fmt.Println("Launching llama-server.exe automatically ...")
		if err := child.Start(); err != nil {
			return fmt.Errorf("failed to start llama-server.exe: %w", err)
		}
		releaseJob, err = attachKillOnParentExit(child.Process.Pid)
		if err != nil {
			log.Printf("Warning: Windows child lifetime guard unavailable: %v", err)
		}
		if releaseJob != nil {
			defer releaseJob()
		}
		defer func() { _ = child.Process.Kill() }()
		go func() { childExited <- child.Wait() }()
	}
	startupCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	if err := waitForHealthy(startupCtx, o.llamaPort, childExited); err != nil {
		return err
	}
	ready.Store(true)
	fmt.Println("")
	fmt.Println("==========================================================")
	fmt.Println("READY! Use this ASR endpoint in VoiceRewrite: http://" + o.listen + "/v1")
	fmt.Println("Qwen3-ASR language tags are removed for Chinese, English, etc.")
	fmt.Println("Leave this window open; press Ctrl+C to stop both services.")
	fmt.Println("==========================================================")
	select {
	case <-ctx.Done():
		log.Println("Stopping launcher and llama.cpp")
	case err := <-childExited:
		return fmt.Errorf("llama-server.exe stopped unexpectedly: %v", err)
	case err := <-errs:
		return err
	}
	return nil
}

func main() {
	file := flag.String("config", "", "path to config.json (default: next to this executable)")
	flag.Parse()
	directory, err := exeDir()
	if err != nil {
		log.Fatal(err)
	}
	name := *file
	if name == "" {
		name = filepath.Join(directory, "config.json")
	}
	cfg, err := loadConfig(name)
	if err == nil && runtime.GOOS == "windows" && alreadyRunning(cfg.ListenAddress) {
		fmt.Println("Qwen3-ASR 服务已经在运行：http://" + cfg.ListenAddress + "/v1")
		return
	}
	if err == nil && runtime.GOOS == "windows" {
		var unlock func()
		unlock, err = acquireLaunchLock()
		if err == nil { defer unlock() }
	}
	if err == nil {
		cfg, err = bootstrapWindows(cfg, directory)
	}
	if err == nil {
		err = run(cfg.toOptions(filepath.Dir(name)))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		if runtime.GOOS == "windows" {
			fmt.Println("Press ENTER to close ...")
			_, _ = fmt.Scanln()
		}
		os.Exit(1)
	}
}
