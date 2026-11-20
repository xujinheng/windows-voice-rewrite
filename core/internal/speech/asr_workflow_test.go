package speech

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/textedit"
)

func TestWAV(t *testing.T) {
	pcm := make([]byte, 32000)
	wav, e := EncodeWAV(pcm)
	if e != nil {
		t.Fatal(e)
	}
	if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || string(wav[36:40]) != "data" {
		t.Fatalf("invalid WAV headers %q", wav[:12])
	}
	if binary.LittleEndian.Uint32(wav[24:28]) != 16000 {
		t.Fatal("sample rate incorrect")
	}
	if len(wav) != 32044 {
		t.Fatal("len", len(wav))
	}
}
func TestAudioAndLLMWorkflow(t *testing.T) {
	asr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("bad route %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Error("bad method")
		}
		if e := r.ParseMultipartForm(3 << 20); e != nil {
			t.Errorf("bad multipart: %v", e)
			return
		}
		if r.FormValue("model") != "qwen3-asr" {
			t.Error("ASR model parameter incorrect")
		}
		if r.FormValue("response_format") != "json" {
			t.Error("response format incorrect")
		}
		f, _, e := r.FormFile("file")
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		if string(b[:4]) != "RIFF" {
			t.Error("no WAV header")
		}
		fmt.Fprint(w, `{"text":"language Chinese<asr_text>请把这句话改得简洁一点。"}`)
	}))
	defer asr.Close()
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"qwen3-asr-1.7b"},{"id":"qwen3-4b-instruct"}]}`)
		case "/v1/chat/completions":
			var b struct {
				Model    string `json:"model"`
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
				t.Error(e)
			}
			if b.Model != "qwen3-4b-instruct" {
				t.Errorf("wrong model %s", b.Model)
			}
			if !strings.Contains(b.Messages[1].Content, "请把这句话改得简洁一点") {
				t.Error("instruction not sent")
			}
			if !strings.Contains(b.Messages[1].Content, "我觉得这个产品的功能其实是比较不错的") {
				t.Error("selected text not sent")
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"这个产品功能不错。"}}]}`)
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer llm.Close()
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL = asr.URL + "/v1"
	cfg.LLMBaseURL = llm.URL + "/v1"
	instruction, e := Transcribe(context.Background(), cfg, make([]byte, 32000))
	if e != nil {
		t.Fatal(e)
	}
	if instruction != "请把这句话改得简洁一点。" {
		t.Errorf("wrong instruction: %q", instruction)
	}
	result, e := textedit.Rewrite(context.Background(), cfg, "我觉得这个产品的功能其实是比较不错的。", instruction)
	if e != nil {
		t.Fatal(e)
	}
	if result != "这个产品功能不错。" {
		t.Errorf("wrong result: %q", result)
	}
}

func TestDictationTranscribesWithoutLLM(t *testing.T) {
	var asrCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		asrCalls++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		if r.FormValue("model") != "qwen3-asr" {
			t.Errorf("model=%s", r.FormValue("model"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"早上好，今天的会议开始。"}`))
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL = server.URL + "/v1"
	cfg.LLMBaseURL = "http://127.0.0.1:1/v1" // must not be called
	pcm := make([]byte, 6400)
	result, err := Transcribe(context.Background(), cfg, pcm)
	if err != nil || result != "早上好，今天的会议开始。" || asrCalls != 1 {
		t.Fatalf("result=%q err=%v calls=%d", result, err, asrCalls)
	}
}

func TestParseASRResponseFormats(t *testing.T) {
	cases := []struct {
		body, ct, expect string
		bad              bool
	}{
		{`{"text":"language Chinese<asr_text>测试"}`, "application/json", "language Chinese<asr_text>测试", false},
		{`language English<asr_text>Hello`, "text/plain; charset=utf-8", "language English<asr_text>Hello", false},
		{`"language German<asr_text>Hallo"`, "application/json", "language German<asr_text>Hallo", false},
		{`{"text":`, "application/json", "", true},
		{`{"oops":`, "text/plain", "", true},
		{`<html>Error</html>`, "text/html", "", true},
	}
	for _, c := range cases {
		got, e := ParseASRResponse([]byte(c.body), c.ct)
		if (e != nil) != c.bad || (!c.bad && got != c.expect) {
			t.Errorf("parse %q: %q / %v", c.body, got, e)
		}
	}
}
