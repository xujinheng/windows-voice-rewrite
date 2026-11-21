package meeting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
)

func TestMeetingCaptionGateDoesNotLeak(t *testing.T) {
	for input, want := range map[string]string{"中文翻译\nG": "中文翻译", "中文翻译\nGATE\tYES": "中文翻译", "中文翻译\nGA": "中文翻译"} {
		if got := CleanCaption(input); got != want {
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
	cfg := coreconfig.Defaults()
	cfg.LLMBaseURL, cfg.TextModel = server.URL, "test"
	var got []Line
	err := Complete(context.Background(), cfg, Query{
		Mode: "caption", Prior: []string{"Previous phrase that must not be recapped"}, Latest: "A pretty.",
	}, func(line Line) { got = append(got, line) })
	if err != nil || len(got) != 1 || got[0].Kind != "ZH" || got[0].Chinese != "一个漂亮的。" {
		t.Fatalf("caption protocol contamination: %#v, %v", got, err)
	}
}

func TestMeetingEnglishExpressionFormats(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
	}{
		{"Could you walk us through the schedule for each option?", "Could you walk us through the schedule for each option?"},
		{"There are several reasons.\nFirst, the age of the data matters.\nSecond, the population definition varies.", "There are several reasons.\r\nFirst, the age of the data matters.\r\nSecond, the population definition varies."},
		{"SAY\tThanks for sharing.\t谢谢分享。", "Thanks for sharing."},
		{"SAY<TAB>Could you clarify the timing?<TAB>能澄清时间吗？", "Could you clarify the timing?"},
		{`{"english":"I'd like to know the schedule."}`, "I'd like to know the schedule."},
		{"<think>internal</think>How long would each option take?", "How long would each option take?"},
		{"GATE<TAB>YES", ""},
		{"只返回中文", ""},
	} {
		if got := EnglishExpression(tc.raw); got != tc.want {
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
				} else if !strings.Contains(request.Messages[0].Content, "general-purpose reasoning") || !strings.Contains(request.Messages[1].Content, "old meeting text") {
					t.Error("intent expression lost meeting context or instruction semantics")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": tc.reply}}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
			}))
			defer server.Close()
			cfg := coreconfig.Defaults()
			cfg.LLMBaseURL, cfg.TextModel = server.URL, "test"
			var got []Line
			err := Complete(context.Background(), cfg, Query{
				Mode: tc.mode, UserPoints: tc.spoken, Prior: []string{"old meeting text"},
			}, func(line Line) { got = append(got, line) })
			if err != nil || len(got) != 1 || !got[0].Complete || got[0].Kind != "SAY" || got[0].English != tc.expected {
				t.Fatalf("English response = %#v, err=%v", got, err)
			}
		})
	}
}

// The rewrite hotkey must not require the user's speech to contain an "ask"
// or "answer" keyword: that meaning belongs in the model's interpretation.
func TestMeetingGeneralPurposeSpokenEnglishRequests(t *testing.T) {
	cases := []struct {
		name, spoken, reply string
		usePrior            bool
	}{
		{"factual information", "有多少印度人会说英语", "India's 2011 census counted around 129 million English speakers, about 10.6% of the population.", false},
		{"unsolicited response", "我不想等他问我，我想补充这个观点", "I'd like to add something here.", true},
		{"clarifying no answer required", "我想问别人周末去哪里玩", "Where are you thinking of going this weekend?", false},
		{"independent topic", "我想说这部电影的结局很有趣", "I thought the ending of that movie was interesting.", false},
		{"reference to context", "问他这两个方案的 schedule", "Could you explain the timeline for both options?", true},
		{"information to say", "我需要回答有多少印度人会说英语", "Based on India's 2011 census, around 129 million people reported speaking English.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if len(request.Messages) != 2 {
					t.Fatalf("unexpected chat body: %+v", request)
				}
				instruction := request.Messages[0].Content
				if !strings.Contains(instruction, "not from fixed command words") ||
					!strings.Contains(instruction, "substantive answer") ||
					!strings.Contains(instruction, "proactively join in") {
					t.Error("prompt presupposes a single question/answer workflow")
				}
				var query Query
				if err := json.Unmarshal([]byte(request.Messages[1].Content), &query); err != nil {
					t.Error(err)
				}
				if query.Mode != "compose" || query.UserPoints != tc.spoken {
					t.Errorf("incorrect route or input: %+v", query)
				}
				if tc.usePrior != (len(query.Prior) > 0) {
					t.Errorf("context relevance failed for %q: %+v", tc.spoken, query)
				}
				if !tc.usePrior && (query.Background != "" || query.Terms != "") {
					t.Errorf("independent speech contaminated by meeting project profile: %+v", query)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": tc.reply}}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
			}))
			defer server.Close()
			cfg := coreconfig.Defaults()
			cfg.LLMBaseURL, cfg.TextModel = server.URL, "test"
			cfg.MeetingBackground, cfg.MeetingTerms = "Milvus project milestones", "Milvus"
			var got []Line
			err := Complete(context.Background(), cfg, Query{
				Mode: "compose", UserPoints: tc.spoken, Prior: []string{"Two proposals were discussed"},
			}, func(line Line) { got = append(got, line) })
			if err != nil || len(got) != 1 || got[0].English != tc.reply || !got[0].Complete {
				t.Fatalf("wrong spoken response: %#v, %v", got, err)
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
	cfg := coreconfig.Defaults()
	cfg.LLMBaseURL = server.URL
	cfg.TextModel = "qwen"
	cfg.MeetingTerms = "Milvus"
	var got []Line
	err := Complete(context.Background(), cfg, Query{Mode: "replies", Latest: "What should we check in Milvus?"}, func(l Line) { got = append(got, l) })
	if err != nil || len(got) < 2 || got[0].Complete || !got[len(got)-1].Complete {
		t.Fatalf("%#v %v", got, err)
	}
}

func TestMeetingStreamRejectsTruncationAndHidesThoughtLines(t *testing.T) {
	truncated := "data: {\"choices\":[{\"delta\":{\"content\":\"ONE\\tReady\\t好\\n\"}}]}\n\n"
	if err := ReadStream(strings.NewReader(truncated), func(Line) {}); err == nil {
		t.Error("truncated stream accepted")
	}
	var got []Line
	thought := "data: {\"choices\":[{\"delta\":{\"content\":\"<think>\\nONE\\tHidden\\t不显示\\n</think>\\nONE\\tReady\\t好\\n\"}}]}\n\ndata: [DONE]\n\n"
	if err := ReadStream(strings.NewReader(thought), func(l Line) { got = append(got, l) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].English != "Ready" {
		t.Fatalf("thoughts leaked: %#v", got)
	}
}

func TestMeetingStreamIncrementalThinkingAcrossEveryChunkBoundary(t *testing.T) {
	cases := []string{
		"ONE\tReady\t好了\nTWO\tAnother\t另一个\n",
		"<think>analysis\nONE\tHidden\t隐藏\n</think> \r\nONE\tDone\t完成\n",
		"ONE\tPrefix<think>hidden\n</think> after\t原文\n",
		"ONE\tA\t前\n<think>thinking</think> \nTWO\tB\t后\n",
		"ONE\tPartial\t中文<thi",
		"ONE\tPartial\t中文</thi",
		"ONE\tVisible\t保留\n<think>unfinished\nTWO\tHidden\t不显示",
		"ONE\tStandalone\t中文\n</think>\nTWO\tAfter\t之后\n",
		"<think></think>ONE\tClean\t中文",
		"ONE<TAB>English<TAB>中文\nTHREE<TAB>Later<TAB>以后",
	}
	for n, input := range cases {
		var want []Line
		for _, raw := range strings.Split(VisibleText(input), "\n") {
			if line, ok := ParseLine(raw); ok {
				want = append(want, line)
			}
		}
		runes := []rune(input)
		for _, width := range []int{1, 2, 3, 5, 7, len(runes)} {
			var stream strings.Builder
			for i := 0; i < len(runes); i += width {
				end := min(i+width, len(runes))
				chunk, err := json.Marshal(map[string]any{
					"choices": []any{map[string]any{"delta": map[string]string{"content": string(runes[i:end])}}},
				})
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&stream, "data: %s\n\n", chunk)
			}
			stream.WriteString("data: [DONE]\n\n")
			var got []Line
			if err := ReadStream(strings.NewReader(stream.String()), func(line Line) { got = append(got, line) }); err != nil {
				t.Fatalf("case %d, width %d: %v", n, width, err)
			}
			if len(got) != len(want) {
				t.Fatalf("case %d, width %d: lines %#v, want %#v", n, width, got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("case %d, width %d, line %d: %#v, want %#v", n, width, i, got[i], want[i])
				}
			}
		}
	}
}

func TestMeetingStreamLongLineBoundsOnlyUnfinishedTail(t *testing.T) {
	long := "ONE\t" + strings.Repeat("x", 16<<10) + "\t中文"
	frame, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]string{"content": long}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ReadStream(strings.NewReader(fmt.Sprintf("data: %s\n\ndata: [DONE]\n\n", frame)), func(Line) {}); err == nil {
		t.Fatal("long unfinished line was accepted")
	}
	frame, err = json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]string{"content": long + "\n"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []Line
	if err := ReadStream(strings.NewReader(fmt.Sprintf("data: %s\n\ndata: [DONE]\n\n", frame)), func(line Line) { got = append(got, line) }); err != nil || len(got) != 1 {
		t.Fatalf("completed line was rejected: lines=%d err=%v", len(got), err)
	}
}

func TestMeetingPartialReplyNeverPublishesSplitThoughtText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, delta := range []string{
			"<thi", "nk>ONE\tPrivate\t内部\n</thi", "nk>  ONE\tThe fix is",
			" tested.\t已验证。\n",
		} {
			chunk, err := json.Marshal(map[string]any{
				"choices": []any{map[string]any{"delta": map[string]string{"content": delta}}},
			})
			if err != nil {
				t.Error(err)
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.TextModel, cfg.LLMBaseURL = "test-model", server.URL
	var got []Line
	if err := Complete(context.Background(), cfg, Query{Mode: "replies", Latest: "Any update?"}, func(line Line) {
		got = append(got, line)
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Complete || !got[len(got)-1].Complete ||
		got[len(got)-1].English != "The fix is tested." {
		t.Fatalf("missing incremental or completed reply: %#v", got)
	}
	for _, line := range got {
		if strings.Contains(line.English, "Private") || strings.Contains(line.English, "<think>") {
			t.Fatalf("thought content leaked into an incremental reply: %#v", got)
		}
	}
}
