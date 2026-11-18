package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Pinned upstream checksums. A third-party mirror is never trusted without SHA-256.
const (
	llamaRelease = "b12503"
	asrModelSHA = "58e22d0532d4eacaf034cfac17a6fed159f37c41390c710186783be439d1fc57"
	asrProjectorSHA = "46c1d533af3f354ceb37ce855dbceff7da7fa7cf1e6a523df3b13440bd164c0d"
	cpuZipSHA = "64bee9b6fece8e66c665c40290cbb84706af23c670041d024b56a71a39859d77"
	cudaZipSHA = "9df9b6316051ce258debb234b3475762eecd8721e6bb41474f0743d54aad08aa"
	cudaRuntimeZipSHA = "8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6"
)

var bootstrapClient = &http.Client{Transport: &http.Transport{
	Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 40 * time.Second,
}}

type downloadSpec struct { filename, sha string; urls []string }

func modelDownload(filename, sha string) downloadSpec {
	return downloadSpec{filename, sha, []string{
		"https://modelscope.cn/api/v1/models/42ailab/Qwen3-ASR-1.7B-GGUF/repo?Revision=master&FilePath=" + filename,
		"https://modelscope.cn/api/v1/models/ggml-org/Qwen3-ASR-1.7B-GGUF/repo?Revision=master&FilePath=" + filename,
	}}
}

func releaseDownload(asset, sha string) downloadSpec {
	upstream := "https://github.com/ggml-org/llama.cpp/releases/download/" + llamaRelease + "/" + asset
	return downloadSpec{asset, sha, []string{"https://ghfast.top/" + upstream, "https://gh-proxy.com/" + upstream}}
}

func validSHA(path, expected string) bool {
	f, err := os.Open(path)
	if err != nil { return false }
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil { return false }
	return hex.EncodeToString(h.Sum(nil)) == expected
}

// Retain .part across interrupted transfers; publish only verified content.
func downloadVerified(ctx context.Context, dest string, spec downloadSpec) error {
	if validSHA(dest, spec.sha) { fmt.Printf("[已缓存] %s (SHA-256 OK)\n", spec.filename); return nil }
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil { return err }
	if validSHA(dest+".part", spec.sha) {
		if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) { return err }
		return os.Rename(dest+".part", dest)
	}
	var last error
	for _, source := range spec.urls {
		fmt.Printf("[下载] %s\n", spec.filename)
		if err := downloadFrom(ctx, source, dest+".part"); err != nil {
			last = err
			fmt.Printf("[切换下载源] %v\n", err)
			continue
		}
		if !validSHA(dest+".part", spec.sha) {
			last = fmt.Errorf("%s 下载内容 SHA-256 不匹配", spec.filename)
			_ = os.Remove(dest+".part")
			continue
		}
		if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) { return err }
		if err := os.Rename(dest+".part", dest); err != nil { return err }
		fmt.Printf("[完成] %s\n", spec.filename)
		return nil
	}
	return fmt.Errorf("无法下载 %s（已尝试国内下载源）: %w", spec.filename, last)
}

func downloadFrom(ctx context.Context, source, part string) error {
	var offset int64
	if stat, err := os.Stat(part); err == nil { offset = stat.Size() }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil { return err }
	if offset > 0 { req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset)) }
	resp, err := bootstrapClient.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("HTTP %d (%s)", resp.StatusCode, source)
	}
	flags := os.O_CREATE|os.O_WRONLY|os.O_TRUNC
	if resp.StatusCode == http.StatusPartialContent {
		if offset == 0 || !strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", offset)) {
			return errors.New("下载源返回不正确的 Content-Range")
		}
		flags = os.O_CREATE|os.O_WRONLY|os.O_APPEND
	}
	f, err := os.OpenFile(part, flags, 0600)
	if err != nil { return err }
	defer f.Close()
	start := time.Now()
	var received int64
	var lastPrint time.Time
	buffer := make([]byte, 256*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			if _, err := f.Write(buffer[:n]); err != nil { return err }
			received += int64(n)
			if time.Since(lastPrint) >= 3*time.Second {
				fmt.Printf("    已下载 %.1f MB (%.1f MB/s)\n", float64(received+offset)/1e6, float64(received)/1e6/time.Since(start).Seconds())
				lastPrint = time.Now()
			}
		}
		if readErr == io.EOF { break }
		if readErr != nil { return readErr }
	}
	if resp.ContentLength >= 0 && received != resp.ContentLength {
		return fmt.Errorf("下载不完整：%d/%d", received, resp.ContentLength)
	}
	return f.Sync()
}

// Install the complete release and DLLs into staging before exposing a runtime.
func installRuntime(ctx context.Context, root, flavor string) (string, error) {
	target := filepath.Join(root, "runtime", flavor)
	marker := filepath.Join(target, ".installed-"+llamaRelease)
	if _, err := os.Stat(marker); err == nil {
		if exe := locateServer(target); exe != "" { return exe, nil }
	}
	var specs []downloadSpec
	if flavor == "cuda" {
		specs = []downloadSpec{
			releaseDownload("llama-"+llamaRelease+"-bin-win-cuda-12.4-x64.zip", cudaZipSHA),
			releaseDownload("cudart-llama-bin-win-cuda-12.4-x64.zip", cudaRuntimeZipSHA),
		}
	} else {
		specs = []downloadSpec{releaseDownload("llama-"+llamaRelease+"-bin-win-cpu-x64.zip", cpuZipSHA)}
	}
	cache := filepath.Join(root, "downloads")
	for _, spec := range specs {
		if err := downloadVerified(ctx, filepath.Join(cache, spec.filename), spec); err != nil { return "", err }
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil { return "", err }
	stage, err := os.MkdirTemp(filepath.Dir(target), ".unpack-")
	if err != nil { return "", err }
	defer os.RemoveAll(stage)
	for _, spec := range specs {
		if err := unpackZip(filepath.Join(cache, spec.filename), stage); err != nil { return "", err }
	}
	if locateServer(stage) == "" { return "", errors.New("安装包没有 llama-server.exe") }
	if err := os.WriteFile(filepath.Join(stage, ".installed-"+llamaRelease), []byte(flavor+"\n"), 0600); err != nil { return "", err }
	if err := os.RemoveAll(target); err != nil { return "", err }
	if err := os.Rename(stage, target); err != nil { return "", err }
	for _, spec := range specs { _ = os.Remove(filepath.Join(cache, spec.filename)) }
	return locateServer(target), nil
}

func unpackZip(file, root string) error {
	archive, err := zip.OpenReader(file)
	if err != nil { return err }
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.Mode()&os.ModeSymlink != 0 { return fmt.Errorf("压缩包包含符号链接: %s", entry.Name) }
		clean := filepath.Clean(filepath.FromSlash(entry.Name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || strings.Contains(clean, ":") {
			return fmt.Errorf("非法压缩包路径: %s", entry.Name)
		}
		target := filepath.Join(root, clean)
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil { return err }
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil { return err }
		source, err := entry.Open()
		if err != nil { return err }
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil { source.Close(); return err }
		_, copyErr := io.Copy(out, source)
		closeErr := out.Close()
		source.Close()
		if copyErr != nil { return copyErr }
		if closeErr != nil { return closeErr }
	}
	return nil
}

func locateServer(root string) string {
	var found string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil { return nil }
		if !entry.IsDir() && strings.EqualFold(entry.Name(), "llama-server.exe") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func hasNVIDIA() bool {
	if runtime.GOOS != "windows" { return false }
	return exec.Command("nvidia-smi", "-L").Run() == nil
}

// Existing explicit paths remain authoritative: automatic installation only
// replaces default names that are not already present beside the EXE.
func bootstrapWindows(cfg config, directory string) (config, error) {
	if runtime.GOOS != "windows" || cfg.AttachToExistingServer { return cfg, nil }
	root := filepath.Join(directory, "Qwen3-ASR")
	if cfg.ModelPath == "Qwen3-ASR-1.7B-Q8_0.gguf" {
		existing := filepath.Join(directory, cfg.ModelPath)
		if stat, err := os.Stat(existing); err == nil && !stat.IsDir() {
			cfg.ModelPath = existing
		} else {
			spec := modelDownload("Qwen3-ASR-1.7B-Q8_0.gguf", asrModelSHA)
			dest := filepath.Join(root, "models", spec.filename)
			if err := downloadVerified(context.Background(), dest, spec); err != nil { return cfg, err }
			cfg.ModelPath = dest
		}
	}
	if cfg.MMProjPath == "mmproj-Qwen3-ASR-1.7B-bf16.gguf" || cfg.MMProjPath == "mmproj-Qwen3-ASR-1.7B-Q8_0.gguf" {
		existing := filepath.Join(directory, cfg.MMProjPath)
		if stat, err := os.Stat(existing); err == nil && !stat.IsDir() {
			cfg.MMProjPath = existing
		} else {
			spec := modelDownload("mmproj-Qwen3-ASR-1.7B-Q8_0.gguf", asrProjectorSHA)
			dest := filepath.Join(root, "models", spec.filename)
			if err := downloadVerified(context.Background(), dest, spec); err != nil { return cfg, err }
			cfg.MMProjPath = dest
		}
	}
	if cfg.LlamaServerPath != "llama-server.exe" { return cfg, nil }
	existing := filepath.Join(directory, "llama-server.exe")
	if stat, err := os.Stat(existing); err == nil && !stat.IsDir() {
		cfg.LlamaServerPath = existing
		if !hasNVIDIA() { cfg.GPULayers, cfg.AllowCPU = 0, true }
		return cfg, nil
	}
	if hasNVIDIA() {
		if exe, err := installRuntime(context.Background(), root, "cuda"); err == nil {
			if checkGPUs(exe, filepath.Dir(exe), false) == nil {
				cfg.LlamaServerPath, cfg.GPULayers, cfg.AllowCPU = exe, 99, false
				fmt.Println("[设备] NVIDIA CUDA")
				return cfg, nil
			}
		} else { fmt.Printf("[提示] CUDA 下载或安装失败，尝试 CPU：%v\n", err) }
	}
	exe, err := installRuntime(context.Background(), root, "cpu")
	if err != nil { return cfg, err }
	cfg.LlamaServerPath, cfg.GPULayers, cfg.AllowCPU = exe, 0, true
	fmt.Println("[设备] CPU")
	return cfg, nil
}

func alreadyRunning(address string) bool {
	if !strings.HasPrefix(address, "127.0.0.1:") && !strings.HasPrefix(address, "localhost:") { return false }
	client := &http.Client{Timeout: 800*time.Millisecond}
	resp, err := client.Get("http://"+address+"/health")
	if err != nil { return false }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return resp.StatusCode == http.StatusOK && strings.Contains(string(body), "Qwen3-ASR one-click OK")
}
