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

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"sync"
	"time"
)

// DefaultDirectory is beside the executable, or beside the .app bundle on Mac.
func DefaultDirectory() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	if runtime.GOOS == "darwin" && filepath.Base(dir) == "MacOS" &&
		filepath.Base(filepath.Dir(dir)) == "Contents" {
		parent := filepath.Dir(filepath.Dir(filepath.Dir(dir)))
		// /Applications is normally read-only to ordinary users.
		if parent == "/Applications" || parent == "/System/Applications" {
			if home, err := os.UserHomeDir(); err == nil {
				return home
			}
		}
		return parent
	}
	return dir
}

var (
	processMu sync.Mutex
	process   *exec.Cmd
)

// ChildHost supplies OS process settings and lifetime protection. Core owns
// the shared launch, readiness and deployment decisions.
type ChildHost struct {
	Prepare func(*exec.Cmd)
	Guard   func(pid int) (func(), error)
}

func (h *ChildHost) prepare(cmd *exec.Cmd) {
	if h != nil && h.Prepare != nil {
		h.Prepare(cmd)
	}
}

// ErrBusyDifferentDirectory preserves a running task on conflicting requests.
var ErrBusyDifferentDirectory = errors.New("ASR deployment already running in another directory")

// BeginDeploy starts one non-blocking application-owned deployment. Repeated
// requests for the same busy or ready directory reuse the existing task.
func BeginDeploy(ctx context.Context, directory string, host *ChildHost) error {
	if ctx == nil || directory == "" {
		return errors.New("choose an ASR directory")
	}
	progressMu.Lock()
	if progress.Directory == directory && (progress.Busy || progress.Phase == PhaseReady) {
		progressMu.Unlock()
		return nil
	}
	if progress.Busy {
		progressMu.Unlock()
		return ErrBusyDifferentDirectory
	}
	progress = Status{Directory: directory, Phase: PhasePreparing,
		Text: "正在准备 ASR 部署", Busy: true}
	progressMu.Unlock()
	go func() {
		installation, err := install(ctx, directory, host)
		if err == nil {
			progressMu.Lock()
			progress.Phase = PhaseStarting
			progress.Text = "正在启动识别服务，加载模型中…"
			progressMu.Unlock()
			err = Start(ctx, installation, host)
		}
		progressMu.Lock()
		progress.Busy = false
		if err != nil {
			progress.Phase = PhaseFailed
			progress.ErrorCode = "deploy_failed"
			progress.Text = "ASR 部署失败：" + err.Error()
		} else {
			progress.Phase = PhaseReady
			progress.Text = "ASR 已部署并启动"
		}
		progressMu.Unlock()
	}()
	return nil
}

// Resume starts a previously deployed local ASR only when the configured
// endpoint is the managed local service, never for a custom/remote endpoint.
func Resume(ctx context.Context, cfg coreconfig.Config, host *ChildHost) error {
	// A settings save must not kill a server while its model is loading.
	if DeploymentStatus().Busy {
		return nil
	}
	if cfg.ASRBaseURL != coreconfig.Defaults().ASRBaseURL {
		Stop()
		return nil
	}
	directory := cfg.ASRDeployDir
	if directory == "" {
		directory = DefaultDirectory()
	}
	install, ok := Installed(directory)
	if !ok {
		return nil
	}
	return Start(ctx, install, host)
}

// Start serves the built-in Qwen ASR model on the existing default endpoint.
// Clients directly use llama.cpp; Core already normalizes ASR transcript markers.
func Start(ctx context.Context, install Installation, host *ChildHost) error {
	for {
		processMu.Lock()
		if process == nil || process.Path == install.Server {
			break
		}
		old := process
		processMu.Unlock()
		_ = old.Process.Kill()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	var exited chan error
	if process == nil {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:8080", 300*time.Millisecond)
		if err == nil {
			conn.Close()
			processMu.Unlock()
			return errors.New("ASR port 8080 is occupied; stop the other server before Deploy")
		}
		logPath := filepath.Join(filepath.Dir(filepath.Dir(install.Model)), "server.log")
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			processMu.Unlock()
			return err
		}
		cmd := exec.Command(install.Server, "-m", install.Model, "--mmproj", install.Projector,
			"--alias", "qwen3-asr", "-ngl", fmt.Sprint(install.GPULayers),
			"-c", "8192", "-np", "1", "--host", "127.0.0.1", "--port", "8080")
		cmd.Dir = filepath.Dir(install.Server)
		cmd.Stdout, cmd.Stderr = file, file
		host.prepare(cmd)
		if err = cmd.Start(); err != nil {
			file.Close()
			processMu.Unlock()
			return err
		}
		var release func()
		if host != nil && host.Guard != nil {
			release, err = host.Guard(cmd.Process.Pid)
			if err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				_ = file.Close()
				processMu.Unlock()
				return fmt.Errorf("cannot bind ASR child lifetime: %w", err)
			}
		}
		process = cmd
		exited = make(chan error, 1)
		go func() {
			e := cmd.Wait()
			if release != nil {
				release()
			}
			processMu.Lock()
			wasCurrent := process == cmd
			if wasCurrent {
				process = nil
				file.Close()
			}
			processMu.Unlock()
			if wasCurrent {
				progressMu.Lock()
				if progress.Phase == PhaseReady {
					progress.Phase = PhaseFailed
					progress.ErrorCode = "runtime_exited"
					progress.Text = "ASR 服务意外停止，请重新 Deploy"
				}
				progressMu.Unlock()
			}
			exited <- e
		}()
	}
	processMu.Unlock()

	client := http.Client{Timeout: 1200 * time.Millisecond}
	timeout, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, _ := http.NewRequestWithContext(timeout, http.MethodGet, "http://127.0.0.1:8080/health", nil)
		res, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 256))
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case err := <-exited:
			return fmt.Errorf("llama-server stopped before model readiness: %v", err)
		case <-timeout.Done():
			Stop()
			return timeout.Err()
		case <-ticker.C:
			processMu.Lock()
			alive := process != nil
			processMu.Unlock()
			if !alive {
				return errors.New("local ASR server stopped before readiness")
			}
		}
	}
}

// Stop leaves the downloaded files intact and stops only the managed child.
func Stop() {
	// Mark intentional shutdown before the waiter observes process exit.
	progressMu.Lock()
	if !progress.Busy && progress.Phase == PhaseReady {
		progress.Phase, progress.Text = PhaseIdle, "ASR 已停止"
	}
	progressMu.Unlock()
	processMu.Lock()
	p := process
	processMu.Unlock()
	if p != nil {
		_ = p.Process.Kill()
	}
}
