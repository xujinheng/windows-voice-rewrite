package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xujinheng/windows-voice-rewrite/core/asrtext"
	"github.com/xujinheng/windows-voice-rewrite/core/provider"
)

func TestCleanASR(t *testing.T) {
	cases := []struct{ in, want string }{
		{"language Chinese<asr_text>帮我改得简洁一点", "帮我改得简洁一点"},
		{"language English<asr_text>Make this shorter.", "Make this shorter."},
		{"language Japanese<asr_text>短くしてください。", "短くしてください。"},
		{"language English<asr_text>Hello. language Chinese<asr_text>你好。", "Hello. 你好。"},
		{"这是一段正常文字", "这是一段正常文字"},
		{"language Chinese<asr_text>", ""},
	}
	for _, tc := range cases {
		if got := asrtext.Clean(tc.in); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
}
func TestNoASRAsTextModel(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"Qwen3-ASR-1.7B-Q8_0"},{"id":"text-embedding-3-small"}]}`)
	}))
	defer llm.Close()
	cfg := defaults()
	cfg.LLMBaseURL = llm.URL + "/v1"
	_, e := provider.DiscoverModel(context.Background(), cfg)
	if e == nil {
		t.Fatal("must not use ASR or embeddings as text editing model")
	}
}
func TestThinkingTag(t *testing.T) {
	s := provider.ThinkingTag.ReplaceAllString("<think>分析...</think>更简洁。", "")
	if s != "更简洁。" {
		t.Fatal(s)
	}
}

func TestDefaultMouse5Hold(t *testing.T) {
	cfg := defaults()
	if cfg.RecordMode != "hold" || cfg.HotkeyRecord != "Mouse5" {
		t.Fatalf("wrong mouse-first defaults: mode=%q, hotkey=%q", cfg.RecordMode, cfg.HotkeyRecord)
	}
}

func TestTrayMenuRequested(t *testing.T) {
	for _, e := range []struct {
		event uint32
		want  bool
	}{
		{0x0205, true}, {0x007b, true}, {0x0204, false}, {0x0201, false}, {0, false},
	} {
		if got := trayMenuRequested(e.event); got != e.want {
			t.Fatalf("event=0x%x got=%t want=%t", e.event, got, e.want)
		}
	}
}
