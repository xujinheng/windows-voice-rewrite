package meeting

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
)

func TestMeetingStream(t *testing.T) {
	var got []Line
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"ONE\\tWhat changed?\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"\\t有什么变化？\\nTIP\\t确认改动\\n\"}}]}\n\n" +
		"data: [DONE]\n\n"
	err := ReadStream(strings.NewReader(body), func(line Line) { got = append(got, line) })
	if err != nil || len(got) != 2 || got[0].English != "What changed?" || got[0].Chinese != "有什么变化？" {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestMeetingCaptionPlainText(t *testing.T) {
	for input, want := range map[string]string{
		"这是翻译。":                         "这是翻译。",
		"ZH<TAB>这是翻译。":                  "这是翻译。",
		"<think>reasoning</think>这是翻译。": "这是翻译。",
		"<think>unfinished":             "",
	} {
		if got := CleanCaption(input); got != want {
			t.Errorf("%q: got %q, want %q", input, got, want)
		}
	}
}

func TestMeetingCaptionProtocolFromRealTranscript(t *testing.T) {
	cases := map[string]string{
		"同龄人，我觉得。哇，我们看起来真的很像。GATE\tYES":    "同龄人，我觉得。哇，我们看起来真的很像。",
		"我也继承了奶奶的一些特质。\tGATE\tYES":         "我也继承了奶奶的一些特质。",
		"SAY<TAB>一个漂亮的。GATE<TAB>YES":       "一个漂亮的。",
		"GATE\tYES\n中文翻译。":                 "中文翻译。",
		"ZH<TAB>这是翻译。GATE<TAB>NO":          "这是翻译。",
		"不过我觉得我也继承了爸爸的一些特质。\nGATE<TAB>YES": "不过我觉得我也继承了爸爸的一些特质。",
		"GATE<TAB>YES": "",
	}
	for input, want := range cases {
		if got := CleanCaption(input); got != want {
			t.Errorf("%q => %q, want %q", input, got, want)
		}
	}
}

func TestMeetingTranslationDoesNotRepeatPriorParagraph(t *testing.T) {
	source := "Playing games and being competitive, so I think that side of me is similar to him."
	good := "喜欢玩游戏，也有竞争心，所以我觉得自己这一方面和他很像。"
	recap := strings.Repeat("不过我觉得我也继承了爸爸的一些特质，我父亲是个理性的人。", 13)
	if !TranslationFitsSource(source, good) {
		t.Fatal("normal translation rejected")
	}
	if TranslationFitsSource(source, recap) {
		t.Fatal("model recap of prior turns accepted as current caption")
	}
	if !TranslationFitsSource("A pretty.", "一个漂亮的。") {
		t.Fatal("short incomplete phrase rejected")
	}
}

func TestMeetingCaptionWaitsForCompleteStream(t *testing.T) {
	firstChunk := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n")
		w.(http.Flusher).Flush()
		close(firstChunk)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"世界\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out := make(chan Line, 3)
	done := make(chan error, 1)
	go func() {
		done <- Complete(ctx, coreconfig.Config{LLMBaseURL: server.URL, TextModel: "test"}, Query{Mode: "caption", Latest: "Hello world"}, func(line Line) {
			out <- line
		})
	}()
	select {
	case <-firstChunk:
	case <-ctx.Done():
		t.Fatal("server never streamed first chunk")
	}
	select {
	case line := <-out:
		t.Fatalf("unfinished translation leaked to reader: %#v", line)
	case <-time.After(50 * time.Millisecond):
	}
	release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-out:
		if line.Chinese != "你好世界" {
			t.Fatalf("unexpected committed translation: %#v", line)
		}
	default:
		t.Fatal("completed translation was never shown")
	}
	select {
	case line := <-out:
		t.Fatalf("translation emitted more than once: %#v", line)
	default:
	}
}
