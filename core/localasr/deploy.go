package localasr

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
	"sync"
	"time"
)

// Pinned upstream checksums. A third-party mirror is never trusted without SHA-256.
const (
	llamaRelease         = "b12503"
	asrModelSHA          = "58e22d0532d4eacaf034cfac17a6fed159f37c41390c710186783be439d1fc57"
	asrProjectorFilename = "mmproj-Qwen3-ASR-1.7B-bf16.gguf"
	asrProjectorSHA      = "8882e9ddab3186f9aa71b1417c847177913e1466655ac944cf86e9b846735d62"
	macArmTarSHA         = "e88d7e91198d0de6ceb3d33711ac01ee0d5b519cd9ba4e81854e3bba84f22214"
	macIntelTarSHA       = "4c42388726490b1ed7864dc621dd29526f0ced3d617a96c5ac111287a7a68ba5"
	cpuZipSHA            = "64bee9b6fece8e66c665c40290cbb84706af23c670041d024b56a71a39859d77"
	cudaZipSHA           = "9df9b6316051ce258debb234b3475762eecd8721e6bb41474f0743d54aad08aa"
	cudaRuntimeZipSHA    = "8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6"
)

var bootstrapClient = &http.Client{Transport: &http.Transport{
	Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 40 * time.Second,
}}

type downloadSpec struct {
	filename, sha string
	urls          []string
}

func modelDownload(filename, sha string) downloadSpec {
	urls := []string{"https://modelscope.cn/api/v1/models/ggml-org/Qwen3-ASR-1.7B-GGUF/repo?Revision=master&FilePath=" + filename}
	if filename != asrProjectorFilename {
		// The 42ailab backup contains Q8_0 weights, but not the bf16 projector.
		urls = append([]string{"https://modelscope.cn/api/v1/models/42ailab/Qwen3-ASR-1.7B-GGUF/repo?Revision=master&FilePath=" + filename}, urls...)
	}
	return downloadSpec{filename, sha, urls}
}

func releaseDownload(asset, sha string) downloadSpec {
	upstream := "https://github.com/ggml-org/llama.cpp/releases/download/" + llamaRelease + "/" + asset
	return downloadSpec{asset, sha, []string{"https://ghfast.top/" + upstream, "https://gh-proxy.com/" + upstream, upstream}}
}

func validSHA(path, expected string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == expected
}

// Retain .part across interrupted transfers; publish only verified content.
func downloadVerified(ctx context.Context, dest string, spec downloadSpec) error {
	report(PhaseVerifying, "正在校验 "+spec.filename)
	if validSHA(dest, spec.sha) {
		report(PhaseVerifying, "已缓存 "+spec.filename)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	report(PhaseVerifying, "正在校验未完成下载 "+spec.filename)
	if validSHA(dest+".part", spec.sha) {
		if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return os.Rename(dest+".part", dest)
	}
	var last error
	for _, source := range spec.urls {
		report(PhaseDownloading, "正在连接 "+spec.filename)
		if err := downloadFrom(ctx, source, dest+".part", spec.filename); err != nil {
			last = err
			report(PhaseDownloading, "下载源不可用，正在切换："+spec.filename)
			continue
		}
		report(PhaseVerifying, "正在校验 "+spec.filename)
		if !validSHA(dest+".part", spec.sha) {
			last = fmt.Errorf("%s 下载内容 SHA-256 不匹配", spec.filename)
			_ = os.Remove(dest + ".part")
			continue
		}
		if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(dest+".part", dest); err != nil {
			return err
		}
		report(PhaseVerifying, "下载完成 "+spec.filename)
		return nil
	}
	return fmt.Errorf("下载 %s 失败：%w。请检查网络/代理后重试；或从 %s 手动下载同名文件放到 %s，再点 Deploy（会校验 SHA-256）", spec.filename, last, spec.urls[len(spec.urls)-1], dest)
}

func downloadFrom(ctx context.Context, source, part, filename string) error {
	var offset int64
	if stat, err := os.Stat(part); err == nil {
		offset = stat.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := bootstrapClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("HTTP %d (%s)", resp.StatusCode, source)
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if resp.StatusCode == http.StatusPartialContent {
		if offset == 0 || !strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", offset)) {
			return errors.New("下载源返回不正确的 Content-Range")
		}
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(part, flags, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	var received int64
	var lastUpdate time.Time
	total := resp.ContentLength
	if resp.StatusCode == http.StatusPartialContent {
		total += offset
	} else {
		offset = 0
	}
	report(PhaseDownloading, "正在下载 "+filename)
	buffer := make([]byte, 256*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			if _, err := f.Write(buffer[:n]); err != nil {
				return err
			}
			received += int64(n)
			if time.Since(lastUpdate) >= 500*time.Millisecond {
				done := received + offset
				reportProgress(filename, done, total)
				lastUpdate = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
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
		if exe := locateServer(target); exe != "" {
			return exe, nil
		}
	}
	var specs []downloadSpec
	if runtime.GOOS == "darwin" {
		asset, sha := "llama-"+llamaRelease+"-bin-macos-arm64.tar.gz", macArmTarSHA
		if runtime.GOARCH == "amd64" {
			asset, sha = "llama-"+llamaRelease+"-bin-macos-x64.tar.gz", macIntelTarSHA
		} else if runtime.GOARCH != "arm64" {
			return "", errors.New("unsupported Mac architecture")
		}
		specs = []downloadSpec{releaseDownload(asset, sha)}
	} else if flavor == "cuda" {
		specs = []downloadSpec{
			releaseDownload("llama-"+llamaRelease+"-bin-win-cuda-12.4-x64.zip", cudaZipSHA),
			releaseDownload("cudart-llama-bin-win-cuda-12.4-x64.zip", cudaRuntimeZipSHA),
		}
	} else {
		specs = []downloadSpec{releaseDownload("llama-"+llamaRelease+"-bin-win-cpu-x64.zip", cpuZipSHA)}
	}
	cache := filepath.Join(root, "downloads")
	for _, spec := range specs {
		if err := downloadVerified(ctx, filepath.Join(cache, spec.filename), spec); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return "", err
	}
	report(PhaseInstalling, "正在解压 llama.cpp 运行时")
	stage, err := os.MkdirTemp(filepath.Dir(target), ".unpack-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	for _, spec := range specs {
		archive := filepath.Join(cache, spec.filename)
		if strings.HasSuffix(archive, ".tar.gz") {
			output, err := exec.Command("tar", "-xzf", archive, "-C", stage).CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("unpack %s: %w: %s", spec.filename, err, output)
			}
		} else if err := unpackZip(archive, stage); err != nil {
			return "", err
		}
	}
	if locateServer(stage) == "" {
		return "", errors.New("安装包没有 llama-server")
	}
	if err := os.WriteFile(filepath.Join(stage, ".installed-"+llamaRelease), []byte(flavor+"\n"), 0600); err != nil {
		return "", err
	}
	if err := os.RemoveAll(target); err != nil {
		return "", err
	}
	if err := os.Rename(stage, target); err != nil {
		return "", err
	}
	for _, spec := range specs {
		_ = os.Remove(filepath.Join(cache, spec.filename))
	}
	return locateServer(target), nil
}

func unpackZip(file, root string) error {
	archive, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("压缩包包含符号链接: %s", entry.Name)
		}
		clean := filepath.Clean(filepath.FromSlash(entry.Name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || strings.Contains(clean, ":") {
			return fmt.Errorf("非法压缩包路径: %s", entry.Name)
		}
		target := filepath.Join(root, clean)
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(out, source)
		closeErr := out.Close()
		source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func locateServer(root string) string {
	name := "llama-server.exe"
	if runtime.GOOS == "darwin" {
		name = "llama-server"
	}
	var found string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() && strings.EqualFold(entry.Name(), name) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func hasNVIDIA() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return exec.Command("nvidia-smi", "-L").Run() == nil
}

// Installation is the local backend configuration shared by desktop hosts.
type Installation struct {
	Server    string
	Model     string
	Projector string
	GPULayers int
}

type JobID uint64

type Phase string

const (
	PhaseIdle        Phase = ""
	PhasePreparing   Phase = "preparing"
	PhaseDownloading Phase = "downloading"
	PhaseVerifying   Phase = "verifying"
	PhaseInstalling  Phase = "installing"
	PhaseStarting    Phase = "starting"
	PhaseReady       Phase = "ready"
	PhaseFailed      Phase = "failed"
)

// Status is the sole in-process deployment state, independent of UI windows.
type Status struct {
	ID        JobID
	Phase     Phase
	Text      string
	Directory string
	Busy      bool
	File      string
	Received  int64
	Total     int64
	ErrorCode string
}

var progressMu sync.RWMutex
var progress Status

func DeploymentStatus() Status {
	progressMu.RLock()
	defer progressMu.RUnlock()
	return progress
}

func report(phase Phase, text string) {
	progressMu.Lock()
	progress.Phase, progress.Text = phase, text
	if phase != PhaseDownloading {
		progress.File, progress.Received, progress.Total = "", 0, 0
	}
	progressMu.Unlock()
}

func reportProgress(filename string, received, total int64) {
	progressMu.Lock()
	progress.Phase = PhaseDownloading
	progress.File, progress.Received, progress.Total = filename, received, total
	if total > 0 {
		progress.Text = fmt.Sprintf("下载 %s：%.1f / %.1f MB (%.0f%%)", filename,
			float64(received)/1e6, float64(total)/1e6, 100*float64(received)/float64(total))
	} else {
		progress.Text = fmt.Sprintf("下载 %s：已下载 %.1f MB", filename, float64(received)/1e6)
	}
	progressMu.Unlock()
}

// install installs only into directory/Qwen3-ASR. No application data or
// package-manager folders are modified.
func install(ctx context.Context, directory string) (Installation, error) {
	if directory == "" {
		return Installation{}, errors.New("choose an ASR directory")
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		return Installation{}, errors.New("local ASR is supported on Windows and macOS")
	}
	root := filepath.Join(directory, "Qwen3-ASR")
	model := filepath.Join(root, "models", "Qwen3-ASR-1.7B-Q8_0.gguf")
	projector := filepath.Join(root, "models", asrProjectorFilename)
	for _, spec := range []downloadSpec{
		modelDownload("Qwen3-ASR-1.7B-Q8_0.gguf", asrModelSHA),
		modelDownload(asrProjectorFilename, asrProjectorSHA),
	} {
		if err := downloadVerified(ctx, filepath.Join(root, "models", spec.filename), spec); err != nil {
			return Installation{}, err
		}
	}
	flavor, gpu := "cpu", 0
	if runtime.GOOS == "darwin" {
		flavor, gpu = "metal", 99
	} else if hasNVIDIA() {
		flavor, gpu = "cuda", 99
	}
	server, err := installRuntime(ctx, root, flavor)
	if err != nil && flavor == "cuda" {
		flavor, gpu = "cpu", 0
		server, err = installRuntime(ctx, root, flavor)
	}
	if err != nil {
		return Installation{}, err
	}
	if gpu != 0 && !supportsGPU(server, flavor) {
		gpu = 0
		if flavor == "cuda" {
			server, err = installRuntime(ctx, root, "cpu")
			if err != nil {
				return Installation{}, err
			}
		}
	}
	return Installation{Server: server, Model: model, Projector: projector, GPULayers: gpu}, nil
}

// Installed returns a previously deployed runtime without downloading or
// scanning multi-gigabyte models on every application launch.
func Installed(directory string) (Installation, bool) {
	root := filepath.Join(directory, "Qwen3-ASR")
	model := filepath.Join(root, "models", "Qwen3-ASR-1.7B-Q8_0.gguf")
	projector := filepath.Join(root, "models", asrProjectorFilename)
	for _, path := range []string{model, projector} {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			return Installation{}, false
		}
	}
	flavors := []string{"cpu"}
	if runtime.GOOS == "darwin" {
		flavors = []string{"metal"}
	}
	if runtime.GOOS == "windows" {
		flavors = []string{"cuda", "cpu"}
	}
	for _, flavor := range flavors {
		target := filepath.Join(root, "runtime", flavor)
		if _, err := os.Stat(filepath.Join(target, ".installed-"+llamaRelease)); err != nil {
			continue
		}
		if server := locateServer(target); server != "" {
			gpu := 0
			if flavor != "cpu" && supportsGPU(server, flavor) {
				gpu = 99
			}
			return Installation{Server: server, Model: model, Projector: projector, GPULayers: gpu}, true
		}
	}
	return Installation{}, false
}

func supportsGPU(server, flavor string) bool {
	output, err := exec.Command(server, "--list-devices").CombinedOutput()
	if err != nil {
		return false
	}
	name := "CUDA"
	if flavor == "metal" {
		name = "Metal"
	}
	return strings.Contains(strings.ToLower(string(output)), strings.ToLower(name))
}
