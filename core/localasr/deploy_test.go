package localasr

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestDeployConfiguredDoesNotMutateOnFailure(t *testing.T) {
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL = "http://127.0.0.1:4567/v1"
	got, err := DeployConfigured(context.Background(), cfg, "", nil)
	if err == nil || got != cfg {
		t.Fatalf("failed deploy modified active ASR config: %+v, %v", got, err)
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
