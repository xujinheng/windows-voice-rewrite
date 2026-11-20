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

func TestHeadlessProfileValidatesSettingsAndNeverLeaksKeys(t *testing.T) {
	secret := "keep-me-private"
	tests := []struct {
		config, want string
		valid        bool
	}{
		{`{}`, `"dictation":"RightCommand"`, true},
		{`{"dictation_hotkey":"Mouse4","hotkey_record":"Mouse5","max_record_seconds":30}`, `"maximumSeconds":30`, true},
		{`{"dictation_hotkey":"rightcmd","hotkey_record":"mouse5"}`, `"dictation":"RightCommand"`, true},
		{`{"dictation_hotkey":"mouse4","hotkey_record":"rightalt"}`, `"rewrite":"RightOption"`, true},
		{`{"dictation_hotkey":"Mouse4","hotkey_record":"Mouse4"}`, "invalid_config", false},
		{`{"dictation_hotkey":"LeftCommand"}`, "invalid_config", false},
		{`{"record_mode":"toggle"}`, "invalid_config", false},
		{`{"audio_device_id":0}`, "invalid_config", false},
		{`{"max_record_seconds":181}`, "\"maximumSeconds\":180", true},
		{`{"max_record_seconds":0}`, "\"maximumSeconds\":180", true},
		{`{"max_record_seconds":-1}`, "\"maximumSeconds\":180", true},
		{`{"asr_api_key":"` + secret + `"}`, `"pillColor":[32,36,46]`, true},
	}
	for _, tt := range tests {
		frame, err := json.Marshal(map[string]any{"v": 1, "id": "profile", "op": "host_profile", "config": json.RawMessage(tt.config)})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := serve(bytes.NewReader(append(frame, '\n')), &out); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), tt.want) {
			t.Fatalf("invalid profile result: %s", out.String())
		}
		var result response
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.OK != tt.valid {
			t.Fatalf("unexpected status for %s: %s", tt.config, out.String())
		}
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

func TestHostProfileUsesSharedPasteMinimum(t *testing.T) {
	reply := headlessProfile(request{Config: json.RawMessage(`{}`)})
	if !reply.OK {
		t.Fatalf("profile failed: %+v", reply)
	}
	profile := reply.Result.(map[string]any)["profile"].(map[string]any)
	if got := profile["pasteRestoreMS"]; got != 700 {
		t.Fatalf("paste dwell must match shared minimum, got %v", got)
	}
}
