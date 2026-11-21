package main

import (
	"encoding/json"
	"strings"
	"testing"
)

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
		result := headlessProfile(json.RawMessage(tt.config))
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		output := string(encoded)
		if strings.Contains(output, secret) || !strings.Contains(output, tt.want) {
			t.Fatalf("invalid profile result: %s", output)
		}
		if result.OK != tt.valid {
			t.Fatalf("unexpected status for %s: %s", tt.config, output)
		}
	}
}

func TestHostProfileUsesSharedPasteMinimum(t *testing.T) {
	reply := headlessProfile(json.RawMessage(`{}`))
	if !reply.OK {
		t.Fatalf("profile failed: %+v", reply)
	}
	profile := reply.Result.(map[string]any)["profile"].(map[string]any)
	if got := profile["pasteRestoreMS"]; got != 700 {
		t.Fatalf("paste dwell must match shared minimum, got %v", got)
	}
}

func TestFreshMacConfigIsGoNormalizedAndNotEchoedForExistingUsers(t *testing.T) {
	result := headlessProfile(json.RawMessage(`{}`))
	if !result.OK {
		t.Fatalf("default profile failed: %+v", result.Error)
	}
	configText, ok := result.Result.(map[string]any)["defaultConfig"].(string)
	if !ok || !strings.HasSuffix(configText, "\n") {
		t.Fatalf("missing human-readable config: %T", result.Result)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(configText), &config); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"asr_base_url": "http://127.0.0.1:8080/v1",
		"asr_model": "qwen3-asr",
		"llm_base_url": "http://127.0.0.1:1234/v1",
		"text_model": "auto",
		"dictation_hotkey": "RightCommand",
		"hotkey_record": "RightOption",
		"record_mode": "hold",
		"max_record_seconds": float64(180),
	} {
		if got := config[key]; got != want {
			t.Errorf("%s: got %v, want %v", key, got, want)
		}
	}
	if len(config) < 8 {
		t.Fatalf("defaults missing expected settings: %+v", config)
	}
	if other := headlessProfile(json.RawMessage(`{"asr_api_key":"hidden-secret"}`)); !other.OK {
		t.Fatalf("custom profile unexpectedly failed")
	} else if _, exposed := other.Result.(map[string]any)["defaultConfig"]; exposed {
		t.Fatal("custom user settings must not be echoed by the bridge")
	}
}
