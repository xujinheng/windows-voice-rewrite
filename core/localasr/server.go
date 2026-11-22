package localasr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// DefaultDirectory is beside the executable, or beside the .app bundle on Mac.
func DefaultDirectory() string {
	exe, err := os.Executable()
	if err != nil { return "" }
	dir := filepath.Dir(exe)
	if runtime.GOOS == "darwin" && filepath.Base(dir) == "MacOS" &&
		filepath.Base(filepath.Dir(dir)) == "Contents" {
		return filepath.Dir(filepath.Dir(filepath.Dir(dir)))
	}
	return dir
}

var (
	processMu sync.Mutex
	process *exec.Cmd
	processLog *os.File
)

// Start serves the built-in Qwen ASR model on the existing default endpoint.
// Clients directly use llama.cpp; Core already normalizes ASR transcript markers.
func Start(ctx context.Context, install Installation) error {
	processMu.Lock()
	if process != nil {
		processMu.Unlock()
		return nil
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:8080", 300*time.Millisecond)
	if err == nil {
		conn.Close()
		processMu.Unlock()
		return errors.New("ASR port 8080 is occupied; stop the other server before Deploy")
	}
	logPath := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(install.Model))), "server.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil { processMu.Unlock(); return err }
	cmd := exec.Command(install.Server, "-m", install.Model, "--mmproj", install.Projector,
		"--alias", "qwen3-asr", "-ngl", fmt.Sprint(install.GPULayers),
		"-c", "8192", "-np", "1", "--host", "127.0.0.1", "--port", "8080")
	cmd.Dir = filepath.Dir(install.Server)
	cmd.Stdout, cmd.Stderr = file, file
	if err = cmd.Start(); err != nil {
		file.Close()
		processMu.Unlock()
		return err
	}
	process, processLog = cmd, file
	processMu.Unlock()
	exited := make(chan error, 1)
	go func() {
		e := cmd.Wait()
		processMu.Lock()
		if process == cmd {
			process, processLog = nil, nil
			file.Close()
		}
		processMu.Unlock()
		exited <- e
	}()
	client := http.Client{Timeout: 1200 * time.Millisecond}
	timeout, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	ticker := time.NewTicker(800*time.Millisecond)
	defer ticker.Stop()
	for {
		req, _ := http.NewRequestWithContext(timeout, http.MethodGet, "http://127.0.0.1:8080/health", nil)
		res, e := client.Do(req)
		if e == nil {
			io.Copy(io.Discard, io.LimitReader(res.Body, 256))
			res.Body.Close()
			if res.StatusCode == http.StatusOK { return nil }
		}
		select {
		case e := <-exited:
			return fmt.Errorf("llama-server stopped before model readiness: %v", e)
		case <-timeout.Done():
			Stop()
			return timeout.Err()
		case <-ticker.C:
		}
	}
}

// Stop leaves the downloaded files intact and stops only the managed child.
func Stop() {
	processMu.Lock()
	p := process
	processMu.Unlock()
	if p != nil { _ = p.Process.Kill() }
}

// Ready returns true only for a managed process already started in this session.
func Ready() bool {
	processMu.Lock()
	defer processMu.Unlock()
	return process != nil
}

// ServerPath derives no executable outside the selected installation directory.
func ServerPath(directory string) (string, bool) {
	install, ok := Installed(strings.TrimSpace(directory))
	return install.Server, ok
}
