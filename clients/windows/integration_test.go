package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xujinheng/windows-voice-rewrite/core/asrtext"
	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/serviceprobe"
)

func TestOneClickLanguagePrefixAllLanguages(t *testing.T) {
	cases := []struct{ input, expected string }{
		{"language Chinese<asr_text>你好", "你好"},
		{"language English<asr_text>Hello world", "Hello world"},
		{"language German<asr_text>Hallo!", "Hallo!"},
		{"language Japanese<asr_text>こんにちは", "こんにちは"},
		{"language Chinese,English<asr_text>OK 你好", "OK 你好"},
		{"language None<asr_text>", ""},
		{"language English<asr_text>Hello language Chinese<asr_text>世界", "Hello 世界"},
		{"language Chinese<asr_text>1. language English<asr_text>2.", "1. 2."},
		{"<asr_text>plain", "plain"},
		{"A language learning example", "A language learning example"},
		{"language 正常的中文<asr_text>不删除前面的语言", "language 正常的中文<asr_text>不删除前面的语言"},
		{"I said language Chinese without a marker", "I said language Chinese without a marker"},
	}
	for _, tc := range cases {
		if got := asrtext.Clean(tc.input); got != tc.expected {
			t.Errorf("asrtext.Clean(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestConfigValuesValidation(t *testing.T) {
	cfg := defaults()
	cfg2, e := coreconfig.WithConnectionFields(cfg, [6]string{" http://127.0.0.1:8081/v1/ ", " qwen3-asr ", "secret", "http://127.0.0.1:1234/v1", "", "llm-secret"})
	if e != nil || cfg2.ASRModel != "qwen3-asr" || cfg2.TextModel != "auto" || cfg2.ASRBaseURL != "http://127.0.0.1:8081/v1" {
		t.Fatalf("updated=%+v, error=%v", cfg2, e)
	}
	_, e = coreconfig.WithConnectionFields(cfg, [6]string{"ftp://user:pw@bad/v1", "m", "", "http://localhost:1234/v1", "auto", ""})
	if e == nil {
		t.Fatal("must reject unsupported API URL")
	}
	longKey := strings.Repeat("k", 4001)
	_, e = coreconfig.WithConnectionFields(cfg, [6]string{"http://127.0.0.1:8081/v1", "m", longKey, "http://127.0.0.1:1234/v1", "auto", "  " + longKey})
	if e == nil || e.Error() != "输入内容过长" {
		t.Fatalf("credential length: %v", e)
	}
	_, e = coreconfig.WithConnectionFields(cfg, [6]string{"http://127.0.0.1:8081/v1", "m", strings.Repeat("k", 4000), "http://127.0.0.1:1234/v1", "auto", ""})
	if e != nil {
		t.Fatalf("4000-byte key rejected: %v", e)
	}
}
func TestNativeServiceProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			switch r.URL.Path {
			case "/v1/audio/transcriptions":
				w.WriteHeader(200)
				return
			case "/v1/chat/completions":
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"HELLO"}}]}`))
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	if got := serviceprobe.ProbeModel(context.Background(), "ASR", srv.URL+"/v1", "", "speech-v2"); !strings.Contains(got, "静音测试成功") {
		t.Fatal(got)
	}
	if got := serviceprobe.ProbeModel(context.Background(), "LLM", srv.URL+"/v1", "", "writer-v3"); !strings.Contains(got, "实际改写测试通过") {
		t.Fatal(got)
	}
}
