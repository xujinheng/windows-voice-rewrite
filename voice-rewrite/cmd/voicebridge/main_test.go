package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestHelloProtocol(t *testing.T) {
	var output bytes.Buffer
	input := strings.NewReader("{\"v\":1,\"id\":\"mac-probe\",\"op\":\"hello\"}\n")
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version int             `json:"v"`
		ID      string          `json:"id"`
		OK      bool            `json:"ok"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.ID != "mac-probe" || !got.OK || !bytes.Contains(got.Result, []byte("\"protocol\":1")) {
		t.Fatalf("unexpected response: %s", output.String())
	}
}

func TestBridgeRejectsUnsupportedAndMalformedRequests(t *testing.T) {
	private := "private-auth-token-never-log"
	input := strings.NewReader("{\"v\":1,\"id\":\"a\",\"op\":\"unknown\"}\n" +
		"{\"v\":9,\"id\":\"b\",\"op\":\"hello\"}\n" +
		"{\"v\":1,\"id\":\"c\",\"op\":\"hello\",\"secret\":\"" + private + "\"}\n" +
		"{not-json}\n")
	var output bytes.Buffer
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d responses: %s", len(lines), output.String())
	}
	if strings.Contains(output.String(), private) {
		t.Fatal("request secret leaked into response")
	}
	for i, want := range []string{"unsupported_op", "protocol_version", "\"ok\":true", "invalid_json"} {
		if !strings.Contains(lines[i], want) {
			t.Fatalf("response %d should contain %q: %s", i, want, lines[i])
		}
	}
}

func TestRetiredGUIAndMeetingBridgeOperationsAreExplicitlyUnsupported(t *testing.T) {
	// The headless macOS app has no Meeting client. Windows Meeting keeps its
	// own Go Core pipeline; old GUI-only operations must not become parallel macOS business paths.
	ops := []string{
		"meeting_start", "meeting_feed", "meeting_snapshot", "meeting_stop",
		"meeting_pause", "meeting_resume", "meeting_accept", "meeting_cancel",
		"meeting_view", "meeting_configure", "meeting_demo",
		"meeting_replies", "meeting_refine", "meeting_compose", "meeting_express",
		"selected_task", "probe_asr", "probe_llm",
		"list_asr_models", "list_llm_models", "check_update", "parse_shortcut",
	}
	var input strings.Builder
	for i, op := range ops {
		line, err := json.Marshal(map[string]any{"v": 1, "id": fmt.Sprintf("retired-%d", i), "op": op})
		if err != nil {
			t.Fatal(err)
		}
		input.Write(line)
		input.WriteByte('\n')
	}
	var output bytes.Buffer
	if err := serve(strings.NewReader(input.String()), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != len(ops) {
		t.Fatalf("got %d replies for %d requests", len(lines), len(ops))
	}
	for i, line := range lines {
		var reply response
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.OK || reply.Error == nil || reply.Error.Code != "unsupported_op" ||
			reply.ID != fmt.Sprintf("retired-%d", i) {
			t.Fatalf("%s unexpectedly enabled: %s", ops[i], line)
		}
	}
}
