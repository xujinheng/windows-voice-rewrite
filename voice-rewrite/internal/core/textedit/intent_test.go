package textedit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreconfig "voicerewrite/internal/core/config"
)

func TestIntentsRouting(t *testing.T) {
	for _, tc := range []struct {
		instruction, intent string
		editable            bool
		browser             bool
		want                string
	}{
		{"改得简洁一点", "edit", true, false, "replace"},
		{"改得简洁一点", "edit", false, true, "try_replace"},
		{"改得简洁一点", "edit", false, false, "try_replace"},
		{"这是什么意思？", "answer", true, true, "popup"},
		{"为什么这样写？", "edit", true, true, "popup"},
		{"总结这段话", "summarize", true, true, "popup"},
		{"翻译成中文", "translate", true, false, "replace"},
		{"翻译成中文", "translate", false, true, "try_replace"},
		{"翻译成中文并替换原文", "edit", true, false, "replace"},
		{"replace this with a shorter version", "edit", true, false, "replace"},
		{"总结这段话", "edit", true, true, "popup"},
		{"请写成英文", "translate", true, false, "replace"},
		{"请写成英文", "translate", false, true, "try_replace"},
		{"请写成英文", "translate", false, false, "try_replace"},
		{"翻译成英文", "translate", false, true, "try_replace"},
		{"英文是什么意思？", "answer", true, true, "popup"},
		{"怎么用英文说？", "translate", true, false, "popup"},
		{"把这句话改成英文", "edit", true, false, "replace"},
		{"解释一下这个术语", "edit", true, true, "popup"},
		// Third-party editors may expose unknown Win32 control classes.
		{"请写成英文", "translate", false, false, "try_replace"}, // WeChat
		{"帮我润色一下", "edit", false, false, "try_replace"},     // VS Code
		{"翻译成中文", "translate", false, false, "try_replace"}, // nonnative editor
		{"请写成英文", "answer", false, false, "try_replace"},    // conflicting model intent
		{"改成英文", "ambiguous", false, false, "try_replace"},
		{"这是什么意思", "answer", false, false, "popup"},
		{"为什么会出现这个错误", "answer", false, false, "popup"},
		{"总结这段话", "summarize", false, false, "popup"},
		{"帮我润色一下", "ambiguous", true, true, "replace"},
		{"帮我", "edit", true, false, "popup"},
		{"嗯", "translate", false, true, "popup"},
		{"看一下", "edit", false, false, "popup"},
	} {
		got := RouteTask(AssistantResult{Intent: tc.intent, Result: "text"}, tc.instruction, tc.editable)
		if got != Action(tc.want) {
			t.Errorf("%s intent=%s editable=%t: got %s want %s", tc.instruction, tc.intent, tc.editable, got, tc.want)
		}
	}
}
func TestParseAssistantResult(t *testing.T) {
	for _, s := range []string{
		`{"intent":"answer","result":"解释"}`,
		"```json\n{\"intent\":\"answer\",\"result\":\"解释\"}\n```",
		`<think>analysis</think>{"intent":"answer","result":"解释"}`,
	} {
		got, err := parseAssistantResult(s)
		if err != nil || got.Intent != "answer" || got.Result != "解释" {
			t.Fatalf("%s: %#v %v", s, got, err)
		}
	}
	for _, s := range []string{`{"intent":"hack","result":"test"}`, `{"intent":"edit","result":""}`, `not json`} {
		if _, err := parseAssistantResult(s); err == nil {
			t.Errorf("accepted invalid %q", s)
		}
	}
}
func TestUnifiedAssistantOneCall(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"qwen3-4b-instruct"}]}`)
		case "/v1/chat/completions":
			count++
			var req struct {
				Messages []struct{ Role, Content string } `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if len(req.Messages) != 2 || !strings.Contains(req.Messages[0].Content, "intent") || !strings.Contains(req.Messages[1].Content, "量子纠缠") || !strings.Contains(req.Messages[1].Content, "什么意思") {
				t.Errorf("bad prompt: %#v", req.Messages)
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"intent\":\"answer\",\"result\":\"量子纠缠是...\"}"}}]}`)
		default:
			t.Errorf("wrong path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.LLMBaseURL = server.URL + "/v1"
	result, err := CompleteSelectedTask(context.Background(), cfg, "量子纠缠", "这是什么意思")
	if err != nil || result.Intent != "answer" || count != 1 {
		t.Fatalf("result=%#v err=%v count=%d", result, err, count)
	}
}

func TestModelIntentAndInvalidResultsNeverAuthorizeWrite(t *testing.T) {
	for _, tc := range []struct {
		name        string
		result      AssistantResult
		instruction string
	}{
		{"model edit without user edit", AssistantResult{Intent: "edit", Result: "text"}, "帮我"},
		{"model translate without user edit", AssistantResult{Intent: "translate", Result: "text"}, "这是什么意思"},
		{"unrecognized intent", AssistantResult{Intent: "execute", Result: "text"}, "润色一下"},
		{"empty result", AssistantResult{Intent: "edit"}, "润色一下"},
		{"malformed preview", AssistantResult{Intent: "ambiguous", Result: "untrusted", PreviewOnly: true}, "润色一下"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, editable := range []bool{true, false} {
				if action := RouteTask(tc.result, tc.instruction, editable); action != Popup {
					t.Fatalf("unauthorized action %s", action)
				}
			}
		})
	}
}

func TestCompleteSelectedTaskMalformedOutputs(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		wantError     bool
	}{
		{"plain text", "just replace with this", false},
		{"invalid intent", `{"intent":"delete","result":"data"}`, false},
		{"missing result", `{"intent":"edit"}`, false},
		{"bad result type", `{"intent":"edit","result":2}`, false},
		{"empty", "<think>reasoning</think>", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, tc.content)
			}))
			defer server.Close()
			cfg := coreconfig.Defaults()
			cfg.TextModel = "test-model"
			cfg.LLMBaseURL = server.URL
			got, err := CompleteSelectedTask(context.Background(), cfg, "selected", "润色一下")
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error, got %#v", got)
				}
				return
			}
			if err != nil || !got.PreviewOnly || RouteTask(got, "润色一下", true) != Popup {
				t.Fatalf("unsafe malformed response: %+v %v", got, err)
			}
		})
	}
}

func TestResolveSelectedTaskCorrectsMisclassifiedEdit(t *testing.T) {
	var count int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"intent\":\"answer\",\"result\":\"explanation\"}"}}]}`)
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"Shorter text"}}]}`)
		}
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.TextModel = "test-model"
	cfg.LLMBaseURL = server.URL
	got, err := ResolveSelectedTask(context.Background(), cfg, "Original text", "make it shorter")
	if err != nil || count != 2 || got.Result != "Shorter text" || got.Intent != "edit" ||
		RouteTask(got, "make it shorter", true) != Replace {
		t.Fatalf("result=%+v err=%v calls=%d", got, err, count)
	}
}

func TestSelectionValidationAndCancelBeforeNetwork(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		t.Error("unexpected network request")
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.TextModel = "test-model"
	cfg.LLMBaseURL = server.URL
	cfg.MaxSelectedRunes = 3
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		ctx                   context.Context
		selected, instruction string
	}{
		{context.Background(), "", "润色"},
		{context.Background(), "   ", "润色"},
		{context.Background(), "abc", ""},
		{context.Background(), "abc", "  "},
		{context.Background(), "中文测试", "润色"},
		{ctx, "abc", "润色"},
	} {
		if _, err := CompleteSelectedTask(tc.ctx, cfg, tc.selected, tc.instruction); err == nil {
			t.Errorf("CompleteSelectedTask(%q, %q) succeeded", tc.selected, tc.instruction)
		}
		if _, err := Rewrite(tc.ctx, cfg, tc.selected, tc.instruction); err == nil {
			t.Errorf("Rewrite(%q, %q) succeeded", tc.selected, tc.instruction)
		}
	}
	if _, err := ResolveSelectedTask(ctx, cfg, "abc", "润色"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation result: %v", err)
	}
	if calls != 0 {
		t.Fatalf("%d network requests for invalid inputs", calls)
	}
}

func TestCompleteSelectedTaskProviderEnvelopeErrors(t *testing.T) {
	for _, response := range []string{`{"choices":[]}`, `{"choices":[{"message":{}}]}`, `invalid JSON`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, response)
		}))
		cfg := coreconfig.Defaults()
		cfg.TextModel = "test-model"
		cfg.LLMBaseURL = server.URL
		got, err := CompleteSelectedTask(context.Background(), cfg, "Selected", "explain")
		if err == nil || got.Result != "" {
			t.Errorf("response %q: %+v %v", response, got, err)
		}
		server.Close()
	}
}
