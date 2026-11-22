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
	macDefaultDictation: 54, macDefaultRewrite: 61, "Mouse4": 103, "Mouse5": 104,
	"LeftCommand": 55, "LeftOption": 58, "LeftCtrl": 59, "RightCtrl": 62,
	"F1": 122, "F2": 120, "F3": 99, "F4": 118, "F5": 96,
	"F6": 97, "F7": 98, "F8": 100, "F9": 101, "F10": 109,
	"F11": 103, "F12": 111, "F13": 105, "F14": 107, "F15": 113,
	"F16": 106, "F17": 64, "F18": 79, "F19": 80, "F20": 90,
}

func macKeyCode(s string) int { return macShortcutCodes[s] }

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
	if errDictation != nil || errRewrite != nil || macKeyCode(dictation) == 0 || macKeyCode(rewrite) == 0 || dictation == rewrite {
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
