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
