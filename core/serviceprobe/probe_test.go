package serviceprobe

import (
	"context"
	"encoding/json"
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
	if got := ProbeModel(context.Background(), "ASR", "http://127.0.0.1:1/v1", "", ""); !strings.Contains(got, "模型 ID") {
		t.Fatalf("empty ASR model should require a model ID, got %q", got)
	}
	if got := ProbeModel(context.Background(), "LLM", "https://name:secret@example.com/v1", "", "auto"); !strings.Contains(got, "服务地址无效") {
		t.Fatalf("url accepted: %q", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := ProbeModel(ctx, "ASR", "http://127.0.0.1:1/v1", "", "qwen3-asr"); !strings.Contains(got, "超时") && !strings.Contains(got, "无法连接") {
		t.Fatalf("cancel returned unexpected result: %q", got)
	}
}

func TestWritingProbeUsesActualRewrite(t *testing.T) {
	for _, tc := range []struct {
		name, model, response, want string
		status int
	}{
		{"explicit", "chat", `{"choices":[{"message":{"content":"HELLO"}}]}`, "实际改写测试通过", 200},
		{"auto", "auto", `{"choices":[{"message":{"content":"HELLO"}}]}`, "实际改写测试通过", 200},
		{"forbidden", "chat", "", "API Key", 403},
		{"missing", "chat", "", "HTTP 404", 404},
		{"rate limited", "chat", "", "HTTP 429", 429},
		{"empty", "chat", `{"choices":[{"message":{"content":""}}]}`, "测试失败", 200},
		{"malformed", "chat", `{"data":[]}`, "测试失败", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts, lists := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer token" {
					t.Error("missing bearer token")
				}
				if r.Method == "GET" && r.URL.Path == "/v1/models" {
					lists++
					_, _ = io.WriteString(w, `{"data":[{"id":"chat"}]}`)
					return
				}
				if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				posts++
				var body struct {
					Model string `json:"model"`
					Messages []map[string]string `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != "chat" || len(body.Messages) != 2 || !strings.Contains(body.Messages[1]["content"], "hello") {
					t.Errorf("unexpected rewrite payload: %+v, %v", body, err)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.response)
			}))
			got := ProbeModel(context.Background(), "LLM", srv.URL+"/v1", "token", tc.model)
			srv.Close()
			wantLists := 0
			if tc.model == "auto" { wantLists = 1 }
			if !strings.Contains(got, tc.want) || posts != 1 || lists != wantLists {
				t.Fatalf("got %q, POST=%d GET=%d", got, posts, lists)
			}
		})
	}
}
