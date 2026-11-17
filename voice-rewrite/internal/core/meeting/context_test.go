package meeting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreconfig "voicerewrite/internal/core/config"
)

func TestPrepareQuerySeparatesLiteralSpeechFromIntentAndRelevantHistory(t *testing.T) {
	cfg := coreconfig.Defaults()
	cfg.MeetingBackground = "I manage the release project."
	cfg.MeetingTerms = "张伟 = Zhang Wei\n米尔沃斯 = Milvus\nAPI = Application Programming Interface"
	prior := []string{"old unrelated statement", "the two options have different timelines"}

	literal := PrepareQuery(cfg, Query{Mode: "express", UserPoints: "张伟说周五交付", Prior: prior})
	if len(literal.Prior) != 0 || literal.Background != "" || literal.Terms != "张伟 = Zhang Wei" {
		t.Fatalf("literal translation must be faithful, but preserve spoken names: %+v", literal)
	}
	independent := PrepareQuery(cfg, Query{Mode: "compose", UserPoints: "我要主动提问下周有多少人", Prior: prior})
	if len(independent.Prior) != 0 || independent.Background != "" || independent.Terms != "" {
		t.Fatalf("independent question must not be turned into a project response: %+v", independent)
	}
	relevant := PrepareQuery(cfg, Query{Mode: "compose", UserPoints: "我想回应这两个方案的 API 风险", Prior: prior})
	if len(relevant.Prior) != 2 || relevant.Background != cfg.MeetingBackground ||
		relevant.Terms != "API = Application Programming Interface" {
		t.Fatalf("meeting references should use relevant context and glossary: %+v", relevant)
	}
	caption := PrepareQuery(cfg, Query{Mode: "caption", Latest: "Zhang Wei delivered.", Prior: prior})
	if len(caption.Prior) != 0 || caption.Terms != cfg.MeetingTerms || caption.Background != cfg.MeetingBackground {
		t.Fatalf("caption should see vocabulary but not a transcript recap: %+v", caption)
	}
	replies := PrepareQuery(cfg, Query{Mode: "replies", Latest: "Should we proceed?", Prior: prior})
	if len(replies.Prior) != 2 || replies.Terms != cfg.MeetingTerms || replies.Background != cfg.MeetingBackground {
		t.Fatalf("requested replies must preserve bounded user profile: %+v", replies)
	}
}

func TestRecentContextBoundedAndDoesNotMutateInput(t *testing.T) {
	veryLong := strings.Repeat("界", 2000)
	prior := []string{"old", veryLong, "middle", "most recent"}
	original := append([]string(nil), prior...)
	out := RecentContext(prior)
	if len(out) != MaxContextTurns || out[0] != strings.Repeat("界", MaxContextTurnRunes) ||
		out[1] != "middle" || out[2] != "most recent" {
		t.Fatalf("expected latest three capped turns, got %q", out)
	}
	total := 0
	for _, p := range out {
		total += len([]rune(p))
	}
	if total > MaxContextRunes || strings.Join(prior, "|") != strings.Join(original, "|") {
		t.Fatal("prior exceeded budget or mutated source")
	}
	all := RecentContext([]string{veryLong, veryLong, veryLong})
	total = 0
	for _, p := range all {
		total += len([]rune(p))
	}
	if total != MaxContextRunes {
		t.Fatalf("global context budget was not spent safely: %d", total)
	}
}

func TestMatchingTermsHandlesBilingualNamesAndWordBoundaries(t *testing.T) {
	terms := "张伟 = Zhang Wei\nAPI = Application Programming Interface\nAI = artificial intelligence\n米尔沃斯：Milvus\n" +
		strings.Repeat("x", MaxGlossaryLineRunes+1)
	for _, tc := range []struct {
		input, expected string
	}{
		{"张伟明天会参加", "张伟 = Zhang Wei"},
		{"Could Zhang Wei join?", "张伟 = Zhang Wei"},
		{"Could we review the API?", "API = Application Programming Interface"},
		{"I sent an email", ""},
		{"我们在使用米尔沃斯", "米尔沃斯：Milvus"},
		{"I'd like to discuss dinner", ""},
	} {
		if got := MatchingTerms(terms, tc.input); got != tc.expected {
			t.Errorf("input %q: got %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestPrepareQueryCapsPersistedProfileAndPreservesSource(t *testing.T) {
	cfg := coreconfig.Defaults()
	cfg.MeetingBackground = strings.Repeat("甲", MaxBackgroundRunes+123)
	cfg.MeetingTerms = strings.Repeat("乙", MaxTermsRunes+123)
	original := cfg.MeetingBackground
	q := PrepareQuery(cfg, Query{Mode: "caption", Latest: "sample"})
	if len([]rune(q.Background)) != MaxBackgroundRunes ||
		len([]rune(q.Terms)) != MaxTermsRunes || cfg.MeetingBackground != original {
		t.Fatal("profile must be bounded without altering the stored configuration")
	}
}

func TestCompleteInjectsOnlyRelevantGlossaryIntoExpressAndCompose(t *testing.T) {
	for _, mode := range []string{"express", "compose"} {
		t.Run(mode, func(t *testing.T) {
			var captured Query
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				if len(req.Messages) != 2 {
					t.Errorf("messages = %d", len(req.Messages))
				} else if err := json.Unmarshal([]byte(req.Messages[1].Content), &captured); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": "Zhang Wei will join on Friday."}}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
			}))
			defer server.Close()
			cfg := coreconfig.Defaults()
			cfg.TextModel, cfg.LLMBaseURL = "test", server.URL
			cfg.MeetingBackground = "Secret unrelated meeting details"
			cfg.MeetingTerms = "张伟 = Zhang Wei\n米尔沃斯 = Milvus"
			var result []Line
			err := Complete(context.Background(), cfg, Query{
				Mode: mode, UserPoints: "张伟周五加入", Prior: []string{"Secret previous transcript"},
			}, func(line Line) { result = append(result, line) })
			if err != nil || len(result) != 1 || result[0].English != "Zhang Wei will join on Friday." {
				t.Fatalf("result %#v, error %v", result, err)
			}
			if captured.Terms != "张伟 = Zhang Wei" || captured.Background != "" ||
				len(captured.Prior) != 0 {
				t.Fatalf("unrelated background/history leaked into %s: %+v", mode, captured)
			}
		})
	}
}
