package main

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
	"strings"
	"sync/atomic"
	"testing"
)

func bootstrapDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

func TestBootstrapDownloadIdempotentAndRepair(t *testing.T) {
	data := []byte("verified model fixture")
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(data)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "models", "model.gguf")
	spec := downloadSpec{filename:"model.gguf", sha:bootstrapDigest(data), urls:[]string{server.URL}}
	for i:=0; i<2; i++ {
		if err:=downloadVerified(context.Background(), path, spec); err!=nil { t.Fatal(err) }
	}
	if hits.Load()!=1 { t.Fatalf("repeat launch redownloaded: %d", hits.Load()) }
	if err:=os.WriteFile(path, []byte("corrupt"), 0600); err!=nil { t.Fatal(err) }
	if err:=downloadVerified(context.Background(), path, spec); err!=nil { t.Fatal(err) }
	if hits.Load()!=2 || !validSHA(path, spec.sha) { t.Fatal("corrupted file not repaired") }
}

func TestBootstrapResume(t *testing.T) {
	data := []byte("1234567890abcdef")
	var rangeSeen atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range")=="bytes=6-" {
			rangeSeen.Store(true)
			w.Header().Set("Content-Range", "bytes 6-15/16")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[6:])
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "model")
	if err:=os.WriteFile(path+".part", data[:6], 0600); err!=nil { t.Fatal(err) }
	if err:=downloadVerified(context.Background(), path, downloadSpec{"model",bootstrapDigest(data),[]string{server.URL}});err!=nil {t.Fatal(err)}
	if !rangeSeen.Load() || !validSHA(path,bootstrapDigest(data)) {t.Fatal("resume failed")}
}

func TestBootstrapRejectsBadSHA(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {io.WriteString(w,"tampered")}))
	defer server.Close()
	dest:=filepath.Join(t.TempDir(),"model.gguf")
	spec:=downloadSpec{"model.gguf",bootstrapDigest([]byte("trusted")),[]string{server.URL}}
	if err:=downloadVerified(context.Background(),dest,spec);err==nil {t.Fatal("accepted invalid SHA")}
	if _,err:=os.Stat(dest);!os.IsNotExist(err){t.Fatalf("published invalid file: %v",err)}
}

func TestBootstrapZipSlip(t *testing.T) {
	dir:=t.TempDir()
	archivePath:=filepath.Join(dir,"fixture.zip")
	writeZip:=func(name string) {
		t.Helper()
		f,err:=os.Create(archivePath);if err!=nil{t.Fatal(err)}
		w:=zip.NewWriter(f);entry,err:=w.Create(name);if err!=nil{t.Fatal(err)}
		_,_=entry.Write([]byte("mock"))
		if err:=w.Close();err!=nil{t.Fatal(err)}
		if err:=f.Close();err!=nil{t.Fatal(err)}
	}
	writeZip("../outside.exe")
	if err:=unpackZip(archivePath,filepath.Join(dir,"out"));err==nil{t.Fatal("allowed zip-slip")}
	writeZip("llama-server.exe")
	if err:=unpackZip(archivePath,filepath.Join(dir,"out"));err!=nil{t.Fatal(err)}
	if locateServer(filepath.Join(dir,"out"))==""{t.Fatal("missing server after extraction")}
}

func TestBootstrapProjectorUsesBF16(t *testing.T) {
	if got := defaultConfig().MMProjPath; got != asrProjectorFilename {
		t.Fatalf("default mmproj = %q, want %q", got, asrProjectorFilename)
	}
	if asrProjectorSHA != "8882e9ddab3186f9aa71b1417c847177913e1466655ac944cf86e9b846735d62" {
		t.Fatal("bf16 projector must use the upstream SHA-256 checksum")
	}
	spec := modelDownload(asrProjectorFilename, asrProjectorSHA)
	if spec.filename != asrProjectorFilename || spec.sha != asrProjectorSHA || len(spec.urls) == 0 {
		t.Fatalf("wrong bf16 projector download: %+v", spec)
	}
	for _, source := range spec.urls {
		if strings.Contains(source, "/42ailab/") || !strings.Contains(source, "FilePath="+asrProjectorFilename) {
			t.Fatalf("bf16 projector must come from a source with that file: %s", source)
		}
	}
}

func TestFindModelPathsDoesNotFallBackToQ8Projector(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "Qwen3-ASR-1.7B-Q8_0.gguf")
	if err := os.WriteFile(model, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	q8 := filepath.Join(dir, "mmproj-Qwen3-ASR-1.7B-Q8_0.gguf")
	if err := os.WriteFile(q8, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	opts := options{modelPath: model}
	if err := findModelPaths(dir, &opts); err == nil {
		t.Fatal("selected the obsolete Q8 projector instead of requiring bf16")
	}
	bf16 := filepath.Join(dir, asrProjectorFilename)
	if err := os.WriteFile(bf16, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	opts.mmprojPath = ""
	if err := findModelPaths(dir, &opts); err != nil || opts.mmprojPath != bf16 {
		t.Fatalf("bf16 projector not selected: path=%q err=%v", opts.mmprojPath, err)
	}
}

func TestBootstrapDomesticSources(t *testing.T) {
	for _,u:=range modelDownload("model.gguf","sha").urls{
		if !strings.HasPrefix(u,"https://modelscope.cn/"){t.Fatal(u)}
	}
	for _,u:=range releaseDownload("example.zip","sha").urls{
		if strings.HasPrefix(u,"https://github.com/"){t.Fatal(u)}
	}
	if alreadyRunning("0.0.0.0:8081"){t.Fatal("never check public network")}
}
