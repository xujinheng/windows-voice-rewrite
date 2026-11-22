package main

import (
	"encoding/json"
	"strings"
	"testing"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/ux"
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
		{`{"dictation_hotkey":"LeftCommand"}`, `"dictation":"LeftCommand"`, true},
		{`{"dictation_hotkey":"F10","hotkey_record":"F11"}`, `"rewrite":"F11"`, true},
		{`{"dictation_hotkey":"F21"}`, "invalid_config", false},
		{`{"record_mode":"toggle"}`, "invalid_config", false},
		{`{"audio_device_id":0}`, `"microphoneID":0`, true},
		{`{"audio_device_id":300}`, "invalid_config", false},
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
	if profile["applyLabel"] != ux.ApplyAction || profile["applyUnsafeLabel"] != ux.ApplyUnsafe {
		t.Fatal("preview action labels must come from Go Core")
	}
}

func TestFreshMacConfigUsesCoreMarshalWithoutEchoingUserSettings(t *testing.T) {
	fresh := headlessProfile(json.RawMessage(`{}`))
	if !fresh.OK {
		t.Fatal(fresh.Error)
	}
	want, err := coreconfig.Marshal(fresh.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := fresh.Result.(map[string]any)["defaultConfig"]; got != string(want) {
		t.Fatalf("generated config differs from Go Core Marshal")
	}
	user := headlessProfile(json.RawMessage(`{"asr_api_key":"hidden-secret"}`))
	if !user.OK {
		t.Fatal(user.Error)
	}
	if _, exposed := user.Result.(map[string]any)["defaultConfig"]; exposed {
		t.Fatal("existing user config must not be echoed")
	}
}
