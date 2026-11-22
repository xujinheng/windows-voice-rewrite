package localasr

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
)

func TestDownloadVerifiedCachedAndRepair(t *testing.T) {
	data := []byte("verified model bytes")
	hash := sha256.Sum256(data)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "models", "model.gguf")
	spec := downloadSpec{filename: "model.gguf", sha: hex.EncodeToString(hash[:]), urls: []string{server.URL}}
	for i := 0; i < 2; i++ {
		if err := downloadVerified(context.Background(), target, spec); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("cached download fetched %d times", requests.Load())
	}
	if err := os.WriteFile(target, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := downloadVerified(context.Background(), target, spec); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || !validSHA(target, spec.sha) {
		t.Fatal("corrupt model was not repaired")
	}
}

func TestUnpackZipCannotEscapeFolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("../escape")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "fixture")
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unpackZip(path, t.TempDir()); err == nil {
		t.Fatal("archive escaped deployment directory")
	}
}

func TestDeployFailureReportsStatus(t *testing.T) {
	if err := Deploy(context.Background(), "", nil); err == nil {
		t.Fatal("expected failure for empty deployment directory")
	}
	progress := ProgressSnapshot()
	if progress.Busy || !strings.Contains(progress.Text, "失败") {
		t.Fatalf("failed Deploy not reported: %+v", progress)
	}
}

func TestResumeDoesNotStartForRemoteASR(t *testing.T) {
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL = "https://example.invalid/v1"
	cfg.ASRDeployDir = t.TempDir()
	if err := Resume(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadProgressAfterResume(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 2<<20)
	half := len(data) / 2
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=1048576-" {
			t.Errorf("expected resumed Range request, got %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 1048576-2097151/2097152")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[half:])
	}))
	defer server.Close()
	part := filepath.Join(t.TempDir(), "model.gguf.part")
	if err := os.WriteFile(part, data[:half], 0600); err != nil {
		t.Fatal(err)
	}
	if err := downloadFrom(context.Background(), server.URL, part, "model.gguf"); err != nil {
		t.Fatal(err)
	}
	if got := ProgressSnapshot().Text; !strings.Contains(got, "model.gguf") || !strings.Contains(got, "%") {
		t.Fatalf("download byte progress unavailable: %q", got)
	}
	if contents, err := os.ReadFile(part); err != nil || !bytes.Equal(contents, data) {
		t.Fatalf("resumed file mismatch: %v", err)
	}
}

func TestConcurrentDeployDoesNotStartAnotherDownload(t *testing.T) {
	deploymentMu.Lock()
	defer deploymentMu.Unlock()
	err := Deploy(context.Background(), t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "正在部署") {
		t.Fatalf("repeated Deploy should leave existing task unchanged: %v", err)
	}
}

func TestRuntimeDownloadHasVerifiedOfficialFallback(t *testing.T) {
	spec := releaseDownload("llama-"+llamaRelease+"-bin-macos-arm64.tar.gz", macArmTarSHA)
	if spec.sha != macArmTarSHA || spec.urls[len(spec.urls)-1] !=
		"https://github.com/ggml-org/llama.cpp/releases/download/"+llamaRelease+"/"+spec.filename {
		t.Fatalf("official verified fallback missing: %+v", spec)
	}
}
