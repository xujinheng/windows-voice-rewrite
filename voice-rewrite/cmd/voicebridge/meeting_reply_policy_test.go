package main

import (
	"context"
	"sync/atomic"
	"testing"

	coreconfig "voicerewrite/internal/core/config"
	meetingcore "voicerewrite/internal/core/meeting"
)

func TestBridgeMeetingManualReplyDefaultAndAutomaticOptIn(t *testing.T) {
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
	m.captions = []meetingcore.Caption{{ID: 7, Source: "meeting turn", Final: true}}
	m.prior = []string{"meeting turn"}
	m.lastFinal = 7
	m.mu.Unlock()

	// A final transcript must not silently use the LLM/GPU by default.
	if err := m.runReplies(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("automatic replies must be opt-in: %d inference calls", got)
	}
	if m.snapshot()["reply_pending"] == true {
		t.Fatal("disabled auto replies produced a pending batch")
	}

	// An explicit user action must always be able to obtain suggestions.
	if err := m.runReplies(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("manual reply did not infer exactly once: %d", got)
	}

	// Enabling automatic suggestions reuses the same pipeline and may
	// generate for a new turn, while preserving the previous readable batch.
	m.mu.Lock()
	m.replies.AutomaticEnabled = true
	m.captions = append(m.captions, meetingcore.Caption{ID: 8, Source: "new turn", Final: true})
	m.prior = meetingcore.RememberPrior(m.prior, "new turn")
	m.lastFinal = 8
	m.mu.Unlock()
	if err := m.runReplies(context.Background(), 8); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("opted-in automatic reply did not infer: %d", got)
	}
}
