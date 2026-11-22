// voicebridge links the shared Go task directly into the macOS native process.
package main

import (
	"encoding/json"
	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/indicator"
	"github.com/xujinheng/windows-voice-rewrite/core/shortcut"
	"github.com/xujinheng/windows-voice-rewrite/core/ux"
	"github.com/xujinheng/windows-voice-rewrite/core/voice"
	"time"
)

const (
	macDefaultDictation, macDefaultRewrite = "RightCommand", "RightOption"

	msgInvalidConfig = "invalid configuration"
)

type bridgeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type response struct {
	OK     bool              `json:"ok"`
	Result any               `json:"result,omitempty"`
	Error  *bridgeError      `json:"error,omitempty"`
	cfg    coreconfig.Config // normalized profile, never sent over the C ABI
}

func failure(code, message string) response {
	return response{Error: &bridgeError{Code: code, Message: message}}
}

func success(value any) response {
	return response{OK: true, Result: value}
}

// Quartz codes remain physical facts owned by the native adapter.
// Quartz virtual-key codes are OS facts; shortcut validation belongs to Core.
var macShortcutCodes = map[string]int{
	macDefaultDictation: 512 + 54, macDefaultRewrite: 512 + 61, "Mouse4": 103, "Mouse5": 104,
	"LeftCommand": 512 + 55, "LeftOption": 512 + 58, "LeftCtrl": 512 + 59, "RightCtrl": 512 + 62,
	"F1": 512 + 122, "F2": 512 + 120, "F3": 512 + 99, "F4": 512 + 118, "F5": 512 + 96,
	"F6": 512 + 97, "F7": 512 + 98, "F8": 512 + 100, "F9": 512 + 101, "F10": 512 + 109,
	"F11": 512 + 103, "F12": 512 + 111, "F13": 512 + 105, "F14": 512 + 107, "F15": 512 + 113,
	"F16": 512 + 106, "F17": 512 + 64, "F18": 512 + 79, "F19": 512 + 80, "F20": 512 + 90,
}

// headlessProfile shares the existing config normalizer and indicator with
// Windows; only physical Mac shortcut identifiers differ.
func headlessProfile(raw json.RawMessage) response {
	var provided map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &provided) != nil || provided == nil {
		return failure("invalid_config", msgInvalidConfig)
	}
	cfg, err := coreconfig.Parse(raw)
	if err != nil {
		return failure("invalid_config", msgInvalidConfig)
	}
	if _, ok := provided["dictation_hotkey"]; !ok {
		cfg.DictationHotkey = macDefaultDictation
	}
	if _, ok := provided["hotkey_record"]; !ok {
		cfg.HotkeyRecord = macDefaultRewrite
	}
	dictation, _, _, _, errDictation := shortcut.Parse(cfg.DictationHotkey)
	rewrite, _, _, _, errRewrite := shortcut.Parse(cfg.HotkeyRecord)
	if errDictation != nil || errRewrite != nil || macShortcutCodes[dictation] == 0 || macShortcutCodes[rewrite] == 0 || dictation == rewrite {
		return failure("invalid_config", msgInvalidConfig)
	}
	cfg.DictationHotkey, cfg.HotkeyRecord = dictation, rewrite
	cfg, err = coreconfig.Normalize(cfg)
	if err != nil || cfg.RecordMode != "hold" || cfg.AudioDeviceID != -1 {
		return failure("invalid_config", msgInvalidConfig)
	}
	result := map[string]any{"profile": map[string]any{
		"dictation": cfg.DictationHotkey, "rewrite": cfg.HotkeyRecord,
		"maximumSeconds": cfg.MaxRecordSeconds, "modifierDelayMS": 200,
		"pasteRestoreMS":   ux.PasteReceiptMinimumMilliseconds,
		"mouseDelayMS":     int(voice.SideHoldThreshold / time.Millisecond),
		"pill":             indicator.HostSpec(),
		"copyLabel":        ux.CopyAction,
		"applyLabel":       ux.ApplyAction,
		"applyUnsafeLabel": ux.ApplyUnsafe,
	}}
	if len(provided) == 0 {
		// Only a new install receives generated defaults. Never echo user credentials.
		data, err := coreconfig.Marshal(cfg)
		if err != nil {
			return failure("invalid_config", msgInvalidConfig)
		}
		result["defaultConfig"] = string(data)
	}
	reply := success(result)
	reply.cfg = cfg
	return reply
}

// Required by Go's c-archive build; the native host calls VRRun directly.
func main() {}
