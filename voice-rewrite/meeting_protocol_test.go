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

func TestMeetingEnglishExpressionFormats(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
	}{
		{"Could you walk us through the schedule for each option?", "Could you walk us through the schedule for each option?"},
		{"SAY\tThanks for sharing.\t谢谢分享。", "Thanks for sharing."},
		{"SAY<TAB>Could you clarify the timing?<TAB>能澄清时间吗？", "Could you clarify the timing?"},
		{`{"english":"I'd like to know the schedule."}`, "I'd like to know the schedule."},
		{"<think>internal</think>How long would each option take?", "How long would each option take?"},
		{"GATE<TAB>YES", ""},
		{"只返回中文", ""},
	} {
		if got := meetingEnglishExpression(tc.raw); got != tc.want {
			t.Errorf("%q -> %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestMeetingLiteralAndIntentEnglishDoNotRequireSAYProtocol(t *testing.T) {
	for _, tc := range []struct {
		mode, spoken, reply, expected string
	}{
		{"express", "我想知道潜在的延期。", "I would like to know about any potential delays.", "I would like to know about any potential delays."},
		{"compose", "问他这两个方案的schedule", "Thanks for sharing the two options. Could you walk us through the schedule and any potential delays for each one?", "Thanks for sharing the two options. Could you walk us through the schedule and any potential delays for each one?"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Messages []struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if len(request.Messages) != 2 || !strings.Contains(request.Messages[1].Content, tc.spoken) {
					t.Errorf("spoken input missing from prompt: %#v", request.Messages)
				}
				if tc.mode == "express" {
					if strings.Contains(request.Messages[1].Content, "old meeting text") || !strings.Contains(request.Messages[0].Content, "Translate") {
						t.Error("literal translation used previous meeting or intent prompt")
					}
				} else if !strings.Contains(request.Messages[0].Content, "INSTRUCTION") || !strings.Contains(request.Messages[1].Content, "old meeting text") {
					t.Error("intent expression lost meeting context or instruction semantics")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": tc.reply}}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
			}))
			defer server.Close()
			cfg := defaults()
			cfg.LLMBaseURL, cfg.TextModel = server.URL, "test"
			var got []meetingLine
			err := completeMeeting(context.Background(), cfg, meetingQuery{
				Mode: tc.mode, UserPoints: tc.spoken, Prior: []string{"old meeting text"},
			}, func(line meetingLine) { got = append(got, line) })
			if err != nil || len(got) != 1 || !got[0].complete || got[0].kind != "SAY" || got[0].english != tc.expected {
				t.Fatalf("English response = %#v, err=%v", got, err)
			}
		})
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
