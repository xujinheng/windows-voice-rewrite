package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// This is an actual subprocess with the production serve(os.Stdin, os.Stdout)
// loop. A client must keep stdin open until its asynchronous response arrives.
// Closing stdin early signals shutdown and cancels in-flight ASR/LLM work.
func TestBridgeSubprocessDelayedProviderReply(t *testing.T) {
	if os.Getenv("VOICEBRIDGE_SUBPROCESS_TEST") == "1" {
		if err := serve(os.Stdin, os.Stdout); err != nil {
			t.Fatal(err)
		}
		return
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			time.Sleep(180 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"text":"rewrite this"}`))
		case "/v1/chat/completions":
			time.Sleep(180 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]string{"content": `{"intent":"edit","result":"Updated text"}`},
				}},
			})
		default:
			http.Error(w, "unexpected endpoint", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := configFor(server.URL)
	cfg["text_model"] = "test-instruct"
	for _, op := range []string{"transcribe", "selected_voice_task"} {
		t.Run(op, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBridgeSubprocessDelayedProviderReply$")
			child.Env = append(os.Environ(), "VOICEBRIDGE_SUBPROCESS_TEST=1")
			var stderr bytes.Buffer
			child.Stderr = &stderr
			stdin, err := child.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := child.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			payload := map[string]any{
				"v": 1, "id": "delayed", "op": op, "config": cfg,
				"selected_text": "Original text", "can_insert": true,
				"pcm_base64": base64.StdEncoding.EncodeToString(make([]byte, 3200)),
			}
			if err := json.NewEncoder(stdin).Encode(payload); err != nil {
				_ = stdin.Close()
				_ = child.Wait()
				t.Fatal(err)
			}
			// Do not close stdin before reading this complete JSONL reply.
			line, readErr := bufio.NewReader(stdout).ReadBytes('\n')
			_ = stdin.Close()
			waitErr := child.Wait()
			if readErr != nil || waitErr != nil {
				t.Fatalf("subprocess failed: read=%v wait=%v stderr=%q line=%q",
					readErr, waitErr, stderr.String(), line)
			}
			var reply response
			if err := json.Unmarshal(bytes.TrimSpace(line), &reply); err != nil {
				t.Fatal(err)
			}
			if !reply.OK || reply.Error != nil || reply.ID != "delayed" {
				t.Fatalf("%s was canceled despite open stdin: %+v", op, reply)
			}
			data, ok := reply.Result.(map[string]any)
			if !ok {
				t.Fatalf("invalid %s result: %#v", op, reply.Result)
			}
			wantRoute, wantText := "insert", "rewrite this"
			if op == "selected_voice_task" {
				wantRoute, wantText = "replace", "Updated text"
			}
			if data["route"] != wantRoute || data["text"] != wantText {
				t.Fatalf("%s: got route=%v text=%v want %s/%s",
					op, data["route"], data["text"], wantRoute, wantText)
			}
			if strings.Contains(stderr.String(), "voicebridge: input/output failure") {
				t.Fatalf("%s subprocess logged IO failure: %q", op, stderr.String())
			}
		})
	}
}
