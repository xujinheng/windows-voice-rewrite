package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreconfig "voicerewrite/internal/core/config"
	meetingcore "voicerewrite/internal/core/meeting"
)

// The macOS bridge must pass completed speech to compose, while the shared
// policy removes unrelated speech. Express remains a literal translation.
func TestBridgeComposeInjectsOnlyReferencedMeetingHistory(t *testing.T) {
	queries := make(chan meetingcore.Query, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) != 2 {
			t.Errorf("unexpected completion body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var q meetingcore.Query
		if err := json.Unmarshal([]byte(body.Messages[1].Content), &q); err != nil {
			t.Errorf("invalid query: %v", err)
		}
		queries <- q
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"finish_reason": "stop",
				"message": map[string]string{"content": "A concrete English answer."}}},
		})
	}))
	defer server.Close()

	cfg := coreconfig.Defaults()
	cfg.TextModel, cfg.LLMBaseURL = "test", server.URL
	cfg.MeetingBackground = "Our team owns the launch timeline."
	cfg.MeetingTerms = "米尔沃斯 = Milvus\nAPI = Application Programming Interface"
	m, err := newBridgeMeeting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.stop()
	m.mu.Lock()
	m.prior = []string{"The previous presenter mentioned Milvus.", "We compared two options."}
	m.mu.Unlock()

	cases := []struct {
		mode, spoken   string
		prior, profile bool
	}{
		{"compose", "我想谈谈昨天看的电影", false, false},
		{"compose", "我想回应这两个方案的风险", true, true},
		{"express", "我想回应这两个方案的风险", false, false},
	}
	for _, tc := range cases {
		var err error
		if tc.mode == "express" {
			err = m.express(context.Background(), tc.spoken)
		} else {
			err = m.compose(context.Background(), tc.spoken)
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.mode, err)
		}
		q := <-queries
		if q.Mode != tc.mode || q.UserPoints != tc.spoken {
			t.Fatalf("incorrect bridge route: %+v", q)
		}
		if (len(q.Prior) > 0) != tc.prior ||
			(q.Background != "") != tc.profile ||
			(q.Terms != "") != tc.profile {
			t.Fatalf("incorrect context policy for %q: %+v", tc.spoken, q)
		}
		if tc.profile && !strings.Contains(q.Terms, "Milvus") {
			t.Fatalf("relevant glossary entry missing: %+v", q)
		}
	}
}

func TestBridgeRefineRequiresCompletedChoiceAndSharedInputLimit(t *testing.T) {
	m, err := newBridgeMeeting(coreconfig.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	defer m.stop()
	m.mu.Lock()
	serial := m.replies.Next()
	m.replies.Publish(serial, meetingcore.ReplyBatch{Lines: [3]meetingcore.Line{
		{Kind: "ONE", English: "A useful option.", Chinese: "一个有用的选择。", Complete: true},
	}})
	m.mu.Unlock()
	if err := m.refine(context.Background(), 0, strings.Repeat("调", meetingcore.RefineFieldLimit+1)); !errors.Is(err, errMeetingInvalidReply) {
		t.Fatalf("bridge accepted an over-limit instruction: %v", err)
	}
	if err := m.refine(context.Background(), 1, "shorter"); !errors.Is(err, errMeetingInvalidReply) {
		t.Fatalf("bridge accepted an incomplete suggestion: %v", err)
	}
	if err := m.setPaused(true); err != nil {
		t.Fatal(err)
	}
	if err := m.refine(context.Background(), 0, "shorter"); !errors.Is(err, errMeetingInvalidReply) {
		t.Fatalf("bridge refined while paused: %v", err)
	}
}
