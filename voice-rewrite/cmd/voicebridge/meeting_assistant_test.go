package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coreconfig "voicerewrite/internal/core/config"
	meetingcore "voicerewrite/internal/core/meeting"
)

func meetingAssistantServer(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) != 2 {
			t.Errorf("bad assistant request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var query meetingcore.Query
		if err := json.Unmarshal([]byte(body.Messages[1].Content), &query); err != nil {
			t.Errorf("invalid meeting query: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var output string
		switch query.Mode {
		case "replies":
			output = "ONE\tI agree with that approach.\t我同意这个方案。\nTWO\tCan you clarify the schedule?\t能确认时间吗？\nTHREE\tLet me check the details.\t我先确认细节。"
			if strings.Contains(query.Latest, "second") {
				output = "ONE\tLet's discuss the second topic.\t讨论第二个话题。"
			}
		case "refine":
			if query.SelectedReply == "" || query.UserPoints == "" {
				t.Error("refinement did not receive selected reply and instruction")
			}
			output = "SAY\tI strongly agree.\t我非常赞同。"
		case "compose":
			if query.UserPoints == "" {
				t.Error("compose did not receive user request")
			}
			output = "India's 2011 census recorded approximately 129 million English speakers."
		default:
			t.Errorf("unexpected inference mode %q", query.Mode)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"finish_reason": "stop",
				"message": map[string]string{"content": output}}},
		})
	}))
}

func TestMeetingAssistantReplyHandoffRefineComposeAndCancellation(t *testing.T) {
	var calls atomic.Int32
	srv := meetingAssistantServer(t, &calls)
	defer srv.Close()
	cfg := coreconfig.Defaults()
	cfg.LLMBaseURL = srv.URL + "/v1"
	cfg.TextModel = "chat-instruct"
	m, err := newBridgeMeeting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.stop()

	m.mu.Lock()
	m.captions = []meetingcore.Caption{{ID: 1, Source: "first meeting statement", Final: true}}
	m.prior = []string{"first meeting statement"}
	m.lastFinal = 1
	m.mu.Unlock()
	if err := m.runReplies(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	first := m.snapshot()
	rows := first["replies"].([]map[string]any)
	if rows[0]["english"] != "I agree with that approach." ||
		rows[1]["chinese"] != "能确认时间吗？" ||
		rows[2]["complete"] != true {
		t.Fatalf("reply suggestions were not complete bilingual options: %#v", rows)
	}
	m.mu.Lock()
	m.captions = append(m.captions, meetingcore.Caption{ID: 2, Source: "second topic", Final: true})
	m.prior = meetingcore.RememberPrior(m.prior, "second topic")
	m.lastFinal = 2
	m.mu.Unlock()
	if err := m.runReplies(context.Background(), 1); err == nil {
		t.Fatal("stale final turn was allowed to generate a reply")
	}
	if err := m.runReplies(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if m.snapshot()["reply_pending"] != true {
		t.Fatal("new batch displaced old readable reply")
	}
	if m.snapshot()["replies"].([]map[string]any)[0]["english"] != "I agree with that approach." {
		t.Fatal("unaccepted batch replaced visible reply")
	}
	if !m.acceptReplies() || m.acceptReplies() {
		t.Fatal("pending batch accept must be one-shot")
	}
	if got := m.snapshot()["replies"].([]map[string]any)[0]["english"]; got != "Let's discuss the second topic." {
		t.Fatalf("wrong accepted reply: %v", got)
	}
	if err := m.refine(context.Background(), 0, "stronger"); err != nil {
		t.Fatal(err)
	}
	if m.snapshot()["expression"].(string) != "I strongly agree." {
		t.Fatalf("selected reply not refined: %#v", m.snapshot())
	}
	if err := m.compose(context.Background(), "有多少印度人会说英语？"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.snapshot()["expression"].(string), "129 million") {
		t.Fatal("independent factual question did not receive an English answer")
	}
	m.cancelWork()
	if m.snapshot()["expression_status"].(string) != meetingcore.WorkCanceled {
		t.Fatal("cancel did not update the shared meeting state")
	}
	if calls.Load() != 4 {
		t.Fatalf("unexpected inference calls: %d", calls.Load())
	}
}

func TestMeetingAssistantBridgeSessionAndValidation(t *testing.T) {
	var calls atomic.Int32
	srv := meetingAssistantServer(t, &calls)
	defer srv.Close()
	h := startBridge(t, defaultOptions())
	cfg := configFor(srv.URL)
	cfg["text_model"] = "chat-instruct"
	h.send(map[string]any{"v": 1, "id": "start", "op": "meeting_start", "config": cfg})
	result := h.receive()
	if !result.OK {
		t.Fatalf("failed to start meeting: %#v", result)
	}
	sid := object(t, result.Result)["session_id"].(string)
	h.send(map[string]any{"v": 1, "id": "compose", "op": "meeting_compose",
		"session_id": sid, "instruction": "给出印度英语人口数字"})
	result = h.receive()
	if !result.OK {
		t.Fatalf("compose via voicebridge failed: %#v", result)
	}
	if !strings.Contains(object(t, result.Result)["expression"].(string), "129 million") {
		t.Fatalf("missing generated expression: %#v", result)
	}
	h.send(map[string]any{"v": 1, "id": "refine-bad", "op": "meeting_refine",
		"session_id": sid, "reply_index": 99, "instruction": "shorter"})
	result = h.receive()
	if result.OK || result.Error == nil || result.Error.Code != "invalid_request" {
		t.Fatalf("invalid reply index allowed: %#v", result)
	}
	h.send(map[string]any{"v": 1, "id": "cancel", "op": "meeting_cancel", "session_id": sid})
	if result = h.receive(); !result.OK {
		t.Fatalf("cancel failed: %#v", result)
	}
	h.send(map[string]any{"v": 1, "id": "stop", "op": "meeting_stop", "session_id": sid})
	if result = h.receive(); !result.OK {
		t.Fatalf("stop failed: %#v", result)
	}
	h.send(map[string]any{"v": 1, "id": "stale", "op": "meeting_compose",
		"session_id": sid, "instruction": "Hello"})
	result = h.receive()
	if result.OK || result.Error == nil || result.Error.Code != "stale_meeting" {
		t.Fatalf("stale expression was accepted: %#v", result)
	}
	h.close()
}

func TestMeetingReplyCoalescesConcurrentRequestsAndAvoidsRepeatInference(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select { case entered <- struct{}{}: default: }
		select {
		case <-release:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{
				map[string]any{"message": map[string]string{"content": "ONE\tA good answer.\t一个好答案。"}}}})
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	cfg := coreconfig.Defaults()
	cfg.LLMBaseURL = srv.URL + "/v1"
	cfg.TextModel = "test-model"
	m, err := newBridgeMeeting(cfg)
	if err != nil { t.Fatal(err) }
	defer m.stop()
	m.mu.Lock()
	m.captions = []meetingcore.Caption{{ID: 5, Source: "latest", Final: true}}
	m.prior = []string{"latest"}
	m.lastFinal = 5
	m.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- m.runReplies(context.Background(), 5) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second): t.Fatal("first reply request never started")
	}
	if err := m.runReplies(context.Background(), 0); err != nil {
		t.Fatal("duplicate manual request:", err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil { t.Fatal("initial reply:", err) }
	case <-time.After(3 * time.Second): t.Fatal("reply did not finish")
	}
	if err := m.runReplies(context.Background(), 5); err != nil { t.Fatal(err) }
	if calls.Load() != 1 {
		t.Fatalf("duplicate automatic turn inferred %d times", calls.Load())
	}
	if err := m.runReplies(context.Background(), 0); err != nil { t.Fatal(err) }
	if calls.Load() != 2 {
		t.Fatalf("manual regeneration should be explicit, got %d calls", calls.Load())
	}
	m.cancelWork()
	if err := m.runReplies(context.Background(), 0); err != nil { t.Fatal(err) }
	if calls.Load() != 3 { t.Fatal("cancel left subsequent requests locked") }
	if err := m.refine(context.Background(), -1, "shorter"); err == nil {
		t.Fatal("invalid reply index was not rejected")
	}
}
