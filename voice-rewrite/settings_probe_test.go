package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreconfig "voicerewrite/internal/core/config"
)

func TestModelProbeStatuses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer demo" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"qwen3.5-9b"},{"id":"qwen3-asr"}]}`)
	}))
	defer server.Close()
	base := server.URL + "/v1"
	checks := []struct{ model, want string }{
		{"qwen3.5-9b", "已在服务中发现"},
		{"unknown", "未列出"},
		{"auto", "可发现 2 个模型"},
	}
	for _, tc := range checks {
		got := uxProbeModel(context.Background(), "LLM", base, "demo", tc.model)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("model=%q got %q", tc.model, got)
		}
	}
	got := uxProbeModel(context.Background(), "LLM", base, "wrong", "qwen3.5-9b")
	if !strings.Contains(got, "API Key") {
		t.Fatal(got)
	}
}

func TestModelProbeRejectsTokenInURL(t *testing.T) {
	if coreconfig.ValidateBaseURL("http://localhost:8080/v1?api_key=secret") == nil {
		t.Fatal("API base URL must not contain query secrets")
	}
	if coreconfig.ValidateBaseURL("https://localhost:8080/v1") != nil {
		t.Fatal("valid local secure URL rejected")
	}
}
