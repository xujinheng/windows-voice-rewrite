package serviceprobe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeStatusLines(t *testing.T) {
	if StatusIdle != "尚未检测连接" || StatusRunning != "正在检测服务…" || StatusStale != "连接配置已修改 · 请重新检测" {
		t.Fatal("probe status")
	}
}

func TestProbeModelMatchesWindowsStatusAndBearerHandling(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{200, "已在服务中发现"},
		{401, "API Key"},
		{404, "不提供模型列表"},
		{503, "暂时不可用"},
		{429, "HTTP 429"},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer token" {
					t.Errorf("unexpected models request: %s %s %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
				}
				w.WriteHeader(tc.status)
				if tc.status == 200 {
					_, _ = io.WriteString(w, `{"data":[{"id":"text-model"}]}`)
				}
			}))
			defer server.Close()
			got := ProbeModel(context.Background(), "LLM", server.URL+"/v1", "token", "text-model")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("probe result %q missing %q", got, tc.want)
			}
		})
	}
}

func TestProbeASRSendsOnlySilentWAVThroughSharedSpeechCore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("wrong endpoint %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("keyless provider unexpectedly received Authorization")
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer file.Close()
		wav, err := io.ReadAll(file)
		if err != nil {
			t.Error(err)
		}
		if len(wav) != 32044 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
			t.Errorf("invalid silent WAV len=%d", len(wav))
		} else {
			for _, sample := range wav[44:] {
				if sample != 0 {
					t.Error("probe uploaded non-silent audio")
					break
				}
			}
		}
		if r.FormValue("model") != "qwen3-asr" {
			t.Errorf("wrong model: %q", r.FormValue("model"))
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	got := ProbeModel(context.Background(), "ASR", server.URL+"/v1", "", "qwen3-asr")
	if !strings.Contains(got, "静音测试成功") {
		t.Fatalf("got %q", got)
	}
}

func TestProbeReportsInvalidURLAndContextCancellation(t *testing.T) {
	if got := ProbeModel(context.Background(), "LLM", "https://name:secret@example.com/v1", "", "auto"); !strings.Contains(got, "服务地址无效") {
		t.Fatalf("url accepted: %q", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := ProbeModel(ctx, "ASR", "http://127.0.0.1:1/v1", "", "qwen3-asr"); !strings.Contains(got, "超时") && !strings.Contains(got, "无法连接") {
		t.Fatalf("cancel returned unexpected result: %q", got)
	}
}

func TestProbeModelsReuseProviderValidationAndDeduplication(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"chat"},{"id":"chat"},{"id":"\ninvalid"},{"id":"text-model"}]}`)
		}
	}))
	defer srv.Close()
	if got := ProbeModel(context.Background(), "LLM", srv.URL+"/v1", "", "auto"); !strings.Contains(got, "2 个模型") {
		t.Fatalf("probe should count usable deduplicated provider models, got %q", got)
	}
	if got := ProbeModel(context.Background(), "LLM", srv.URL+"/v1", "", "text-model"); !strings.Contains(got, "已在服务中发现") {
		t.Fatal(got)
	}
}

func TestProbeModelsKeepMalformedResponseHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":`)
	}))
	defer srv.Close()
	if got := ProbeModel(context.Background(), "LLM", srv.URL+"/v1", "", "auto"); !strings.Contains(got, "模型列表格式无法识别") {
		t.Fatal(got)
	}
}
