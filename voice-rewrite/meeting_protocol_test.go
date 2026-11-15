package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMeetingCaptionGateDoesNotLeak(t *testing.T) {
	for input, want := range map[string]string{"中文翻译\nG": "中文翻译", "中文翻译\nGATE\tYES": "中文翻译", "中文翻译\nGA": "中文翻译"} {
		if got := cleanMeetingCaption(input); got != want {
			t.Errorf("%q got %q", input, got)
		}
	}
}
func TestMeetingCaptionPromptCannotEchoPriorOrControlTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("expected system and user messages, got %d", len(req.Messages))
		}
		if strings.Contains(req.Messages[0].Content, "GATE") || strings.Contains(req.Messages[0].Content, "SAY<TAB>") {
			t.Error("reply/control instructions contaminated caption prompt")
		}
		if strings.Contains(req.Messages[1].Content, "Previous phrase that must not be recapped") || !strings.Contains(req.Messages[1].Content, "A pretty.") {
			t.Error("caption request supplied unrelated prior utterances or lost latest")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": "SAY<TAB>一个漂亮的。GATE<TAB>YES"}}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	}))
	defer server.Close()
	cfg := defaults()
	cfg.LLMBaseURL, cfg.TextModel = server.URL, "test"
	var got []meetingLine
	err := completeMeeting(context.Background(), cfg, meetingQuery{
		Mode: "caption", Prior: []string{"Previous phrase that must not be recapped"}, Latest: "A pretty.",
	}, func(line meetingLine) { got = append(got, line) })
	if err != nil || len(got) != 1 || got[0].kind != "ZH" || got[0].chinese != "一个漂亮的。" {
		t.Fatalf("caption protocol contamination: %#v, %v", got, err)
	}
}

func TestMeetingPartialReplyAndNoThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		kwargs, ok := req["chat_template_kwargs"].(map[string]any)
		if !ok || kwargs["enable_thinking"] != false {
			t.Error("thinking enabled")
		}
		messages, _ := json.Marshal(req["messages"])
		if !strings.Contains(string(messages), "Milvus") {
			t.Error("profile missing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, s := range []string{"ONE\tI'd check ", "the cache.\t我会检查缓存。\n"} {
			b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": s}}}})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	cfg := defaults()
	cfg.LLMBaseURL = server.URL
	cfg.TextModel = "qwen"
	cfg.MeetingTerms = "Milvus"
	var got []meetingLine
	err := completeMeeting(context.Background(), cfg, meetingQuery{Mode: "replies", Latest: "What should we check?"}, func(l meetingLine) { got = append(got, l) })
	if err != nil || len(got) < 2 || got[0].complete || !got[len(got)-1].complete {
		t.Fatalf("%#v %v", got, err)
	}
}

func TestMeetingStreamRejectsTruncationAndHidesThoughtLines(t *testing.T) {
	truncated := "data: {\"choices\":[{\"delta\":{\"content\":\"ONE\\tReady\\t好\\n\"}}]}\n\n"
	if err := readMeetingStream(strings.NewReader(truncated), func(meetingLine) {}); err == nil {
		t.Error("truncated stream accepted")
	}
	var got []meetingLine
	thought := "data: {\"choices\":[{\"delta\":{\"content\":\"<think>\\nONE\\tHidden\\t不显示\\n</think>\\nONE\\tReady\\t好\\n\"}}]}\n\ndata: [DONE]\n\n"
	if err := readMeetingStream(strings.NewReader(thought), func(l meetingLine) { got = append(got, l) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].english != "Ready" {
		t.Fatalf("thoughts leaked: %#v", got)
	}
}
