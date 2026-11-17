package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]string{"content": "ONE\tA good answer.\t一个好答案。"},
				}},
			})
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	cfg := coreconfig.Defaults()
	cfg.LLMBaseURL = srv.URL + "/v1"
	cfg.TextModel = "test-model"
	m, err := newBridgeMeeting(cfg)
	if err != nil {
		t.Fatal(err)
	}
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
	case <-time.After(5 * time.Second):
		t.Fatal("first reply request never started")
	}
	if err := m.runReplies(context.Background(), 0); err != nil {
		t.Fatal("duplicate manual request:", err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("initial reply:", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reply did not finish")
	}
	if err := m.runReplies(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate automatic turn inferred %d times", calls.Load())
	}
	if err := m.runReplies(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("manual regeneration should be explicit, got %d calls", calls.Load())
	}
	m.cancelWork()
	if err := m.runReplies(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatal("cancel left subsequent requests locked")
	}
	if err := m.refine(context.Background(), -1, "shorter"); err == nil {
		t.Fatal("invalid reply index was not rejected")
	}
}

func TestMeetingAssistantRejectsInvalidRequestsBeforeInference(t *testing.T) {
	h := startBridge(t, defaultOptions())
	cfg := configFor("http://127.0.0.1:8080")
	cfg["text_model"] = "chat-instruct"
	h.send(map[string]any{"v": 1, "id": "start", "op": "meeting_start", "config": cfg})
	start := h.receive()
	if !start.OK {
		t.Fatalf("meeting start: %#v", start)
	}
	sid := object(t, start.Result)["session_id"].(string)
	cases := []struct {
		id, op, instruction, message string
		index                        int
	}{
		{"no-final", "meeting_replies", "", meetingcore.RepliesWait, 0},
		{"blank-compose", "meeting_compose", "　 \t ", "2000", 0},
		{"long-compose", "meeting_compose", strings.Repeat("字", 2001), "2000", 0},
		{"blank-refine", "meeting_refine", " \t ", "2000", 0},
		{"long-refine", "meeting_refine", strings.Repeat("a", 2001), "2000", 0},
		{"no-selection", "meeting_refine", "make it shorter", "完整", 0},
	}
	for _, tc := range cases {
		h.send(map[string]any{
			"v": 1, "id": tc.id, "op": tc.op, "session_id": sid,
			"instruction": tc.instruction, "reply_index": tc.index,
		})
		got := h.receive()
		if got.OK || got.Error == nil || got.Error.Code != "invalid_request" ||
			!strings.Contains(got.Error.Message, tc.message) {
			t.Fatalf("%s: expected actionable request rejection, got %#v", tc.id, got)
		}
	}
	h.send(map[string]any{"v": 1, "id": "stop", "op": "meeting_stop", "session_id": sid})
	if got := h.receive(); !got.OK {
		t.Fatalf("stop: %#v", got)
	}
	h.close()
}

func TestMeetingCancelDuringExpressionCannotOverwriteNewAnswer(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) < 2 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var query meetingcore.Query
		if json.Unmarshal([]byte(body.Messages[1].Content), &query) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if query.UserPoints == "old" {
			once.Do(func() { close(started) })
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]string{"content": "New approved answer."},
			}},
		})
	}))
	defer srv.Close()

	h := startBridge(t, defaultOptions())
	cfg := configFor(srv.URL)
	cfg["text_model"] = "test-model"
	h.send(map[string]any{"v": 1, "id": "start", "op": "meeting_start", "config": cfg})
	first := h.receive()
	if !first.OK {
		t.Fatalf("meeting start: %#v", first)
	}
	sid := object(t, first.Result)["session_id"].(string)
	h.send(map[string]any{"v": 1, "id": "old", "op": "meeting_compose",
		"session_id": sid, "instruction": "old"})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("old expression never started")
	}
	h.send(map[string]any{"v": 1, "id": "cancel", "op": "meeting_cancel", "session_id": sid})
	canceled := h.receiveIDs("old", "cancel")
	if !canceled["cancel"].OK || canceled["old"].OK ||
		canceled["old"].Error == nil || canceled["old"].Error.Code != "canceled" {
		t.Fatalf("cancel did not acknowledge stale inference: %#v", canceled)
	}
	h.send(map[string]any{"v": 1, "id": "new", "op": "meeting_compose",
		"session_id": sid, "instruction": "new"})
	result := h.receive()
	if result.ID != "new" || !result.OK {
		t.Fatalf("replacement expression: %#v", result)
	}
	snapshot := object(t, result.Result)
	if snapshot["expression"] != "New approved answer." ||
		snapshot["expression_status"] != "" {
		t.Fatalf("old callback replaced current answer: %#v", snapshot)
	}
	h.send(map[string]any{"v": 1, "id": "stop", "op": "meeting_stop", "session_id": sid})
	if got := h.receive(); !got.OK {
		t.Fatalf("stop: %#v", got)
	}
	h.close()
}

func TestMeetingReplyHandoffAllowsIncompleteFirstWithReadySecondAndTip(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		text := "ONE\tThe first option.\t第一条。"
		switch n {
		case 2:
			text = "TWO\tCould you expand?\t能详细说说吗？\nTIP\t核对实际范围"
		case 3:
			text = "THREE\tLet me verify the numbers.\t我去核对数字。\nTIP\t检查时间区间"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"finish_reason": "stop",
				"message": map[string]string{"content": text},
			}},
		})
	}))
	defer srv.Close()

	cfg := coreconfig.Defaults()
	cfg.LLMBaseURL = srv.URL + "/v1"
	cfg.TextModel = "test-model"
	m, err := newBridgeMeeting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.stop()
	for turn := uint64(1); turn <= 3; turn++ {
		m.mu.Lock()
		m.captions = append(m.captions, meetingcore.Caption{
			ID: turn, Source: "test speech", Final: true,
		})
		m.prior = meetingcore.RememberPrior(m.prior, "test speech")
		m.lastFinal = turn
		m.mu.Unlock()
		if err := m.runReplies(context.Background(), turn); err != nil {
			t.Fatalf("turn %d valid reply was rejected: %v", turn, err)
		}
		snapshot := m.snapshot()
		if turn == 2 {
			if snapshot["reply_pending"] != true {
				t.Fatal("second-only batch is not switchable")
			}
			if snapshot["reply_first_ready"] != 0 {
				t.Fatal("unaccepted batch should preserve the first displayed option")
			}
			if !m.acceptReplies() {
				t.Fatal("could not accept second-only batch")
			}
			snapshot = m.snapshot()
			if snapshot["reply_first_ready"] != 1 ||
				snapshot["reply_tip"] != "核对实际范围" {
				t.Fatalf("second-only reply lost index or hint: %#v", snapshot)
			}
		}
		if turn == 3 {
			if !m.acceptReplies() {
				t.Fatal("third-only reply was not offered")
			}
			snapshot = m.snapshot()
			if snapshot["reply_first_ready"] != 2 ||
				snapshot["reply_tip"] != "检查时间区间" {
				t.Fatalf("third-only batch was not preserved: %#v", snapshot)
			}
		}
	}
}
