package meeting

import (
	coreconfig "voicerewrite/internal/core/config"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMeetingASRStreamsAndRollsBackExactTokens(t *testing.T) {
	ack := make(chan struct{}, 8)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&b)
		switch r.URL.Path {
		case "/tokenize":
			fmt.Fprint(w, `{"tokens":[1,2,3,4,5,6,7]}`)
		case "/detokenize":
			if string(b["tokens"]) != "[1,2]" {
				t.Errorf("rollback %s", b["tokens"])
			}
			fmt.Fprint(w, `{"content":"We should "}`)
		case "/v1/chat/completions":
			calls++
			if !strings.Contains(string(b["messages"]), "input_audio") {
				t.Error("missing audio")
			}
			if calls == 2 {
				var messages []struct {
					Role    string
					Content any
				}
				json.Unmarshal(b["messages"], &messages)
				if messages[len(messages)-1].Content != "language English<asr_text>We should " {
					t.Error("prefix missing")
				}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			text := "language English<asr_text>We should inspect the cache today"
			if calls == 2 {
				text = "check the cache today"
			}
			data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": text}}}})
			fmt.Fprintf(w, "data: %s\n\n", data)
			w.(http.Flusher).Flush()
			select {
			case <-ack:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL = server.URL + "/v1"
	var state ASRState
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		result, err := state.Transcribe(ctx, cfg, make([]byte, 3200), func(s string) {
			if strings.Contains(s, "<asr_text>") {
				t.Error("metadata leaked")
			}
			ack <- struct{}{}
		})
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && result != "We should check the cache today" {
			t.Fatal(result)
		}
	}
}
func TestMeetingASRUnsupportedDoesNotSilentlyChangeMode(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	var state ASRState
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL = server.URL
	_, err := state.Transcribe(context.Background(), cfg, make([]byte, 3200), func(string) {})
	if err == nil || !strings.Contains(err.Error(), "分段") {
		t.Fatalf("%v", err)
	}
}

func TestMeetingASREmptyContinuationCannotCommitPrefix(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/tokenize":
					fmt.Fprint(w, `{"tokens":[1,2,3,4,5,6,7]}`)
				case "/detokenize":
					fmt.Fprint(w, `{"content":"Retained "}`)
				default:
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: [DONE]\n\n")
					} else {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"choices":[]}`)
					}
				}
			}))
			defer server.Close()
			cfg := coreconfig.Defaults()
			cfg.ASRBaseURL = server.URL
			original := "language English<asr_text>Retained and still provisional words"
			state := ASRState{raw: original}
			if _, err := state.Transcribe(context.Background(), cfg, make([]byte, 3200), func(string) {}); err == nil {
				t.Fatal("empty response committed prefix")
			}
			if state.raw != original {
				t.Fatal("state changed on invalid response")
			}
		})
	}
}
