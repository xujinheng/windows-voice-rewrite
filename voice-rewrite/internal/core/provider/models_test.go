package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"strconv"
	"testing"

	coreconfig "voicerewrite/internal/core/config"
)

func TestListModelsValidatesAndBoundsUntrustedProviderData(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer model-key" {
			t.Errorf("unexpected model-list request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"chat-alpha"},{"id":"chat-alpha"},{"id":"  asr-one  "},{"id":"\nprivate"},{"id":""},{"id":"instruct-two"},{"id":"  "}]}`))
	}))
	defer srv.Close()
	models, err := ListModels(context.Background(), srv.URL+"/v1", "model-key")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(models, ","); got != "chat-alpha,asr-one,instruct-two" {
		t.Fatalf("unexpected bounded models: %q", got)
	}
	cfg := coreconfig.Defaults()
	cfg.LLMBaseURL = srv.URL + "/v1"
	cfg.LLMAPIKey = "model-key"
	cfg.TextModel = "auto"
	model, err := DiscoverModel(context.Background(), cfg)
	if err != nil || model != "instruct-two" {
		t.Fatalf("auto discovery must retain shared ranking: %q %v", model, err)
	}
	if calls != 2 {
		t.Fatalf("expected two model-list requests, got %d", calls)
	}
}

func TestDiscoverModelUsesLateTextModelAfterManyASREntries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entries := make([]modelItem, 0, 151)
		for i := range 150 {
			entries = append(entries, modelItem{ID: "asr-" + strconv.Itoa(i)})
		}
		entries = append(entries, modelItem{ID: "chat-instruct-late"})
		_ = json.NewEncoder(w).Encode(map[string]any{"data": entries})
	}))
	defer srv.Close()
	cfg := coreconfig.Defaults()
	cfg.LLMBaseURL = srv.URL + "/v1"
	cfg.TextModel = "auto"
	got, err := DiscoverModel(context.Background(), cfg)
	if err != nil || got != "chat-instruct-late" {
		t.Fatalf("automatic model selection must inspect complete bounded HTTP response: %q %v", got, err)
	}
}

func TestListModelsRejectsRedirectsAndBadEndpoints(t *testing.T) {
	var destinationCalls int
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls++
	}))
	defer destination.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/secret", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	if _, err := ListModels(context.Background(), srv.URL+"/v1", "key"); err == nil {
		t.Fatal("redirect must not be followed or treated as success")
	}
	if destinationCalls != 0 {
		t.Fatal("model discovery leaked a request across provider origins")
	}
	if _, err := ListModels(context.Background(), "http://user:secret@example.test/v1", ""); err == nil {
		t.Fatal("credential-bearing endpoint accepted")
	}
}

func TestListModelsEmptyAndInvalidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	got, err := ListModels(context.Background(), srv.URL+"/v1", "")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty model list must be an empty array: %#v %v", got, err)
	}
}
