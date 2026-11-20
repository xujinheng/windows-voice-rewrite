// voicebridge links the shared Go task directly into the macOS native process.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"voicerewrite/internal/core/audio"
	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/indicator"
	"voicerewrite/internal/core/input"
	"voicerewrite/internal/core/shortcut"
	"voicerewrite/internal/core/ux"
	"voicerewrite/internal/core/voice"
)

const (
	macDefaultDictation, macDefaultRewrite = "RightCommand", "RightOption"

	maxPCMBytes      = audio.PCMBytesPerSecond * coreconfig.RecordSecondsMax
	maxSelectedBytes = 1 << 20
	msgCanceled      = "request canceled"
	msgTimedOut      = "request timed out"
	msgInvalidConfig = "invalid configuration"
)

type request struct {
	Rewrite      bool
	SelectedText string
	NativePCM    []byte
	AfterASR     func(string)
}

type bridgeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type response struct {
	OK     bool         `json:"ok"`
	Result any          `json:"result,omitempty"`
	Error  *bridgeError `json:"error,omitempty"`
	cfg    coreconfig.Config // normalized profile, never sent over the C ABI
}

// The native C ABI uses this boundary.
// It owns the final cancellation result so ASR and rewrite do not each
// duplicate context checks; private provider errors and panics stay contained.
func safeProcessRequest(ctx context.Context, cfg coreconfig.Config, req request) (reply response) {
	reply = failure("internal_error", "request failed")
	defer func() {
		if recover() != nil {
			return
		}
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				reply = failure("timeout", msgTimedOut)
			} else {
				reply = failure("canceled", msgCanceled)
			}
		}
	}()
	if req.Rewrite && (len(req.SelectedText) > maxSelectedBytes ||
		!utf8.ValidString(req.SelectedText)) {
		return failure("invalid_request", "selected text exceeds limits")
	}
	outcome, err := voice.Run(ctx, cfg, req.NativePCM, req.SelectedText, req.Rewrite, req.AfterASR)
	if err != nil {
		var capture *voice.PCMError
		if errors.As(err, &capture) {
			return failure("invalid_pcm", capture.Error())
		}
		code, message := "asr_failed", "speech transcription failed"
		if req.Rewrite {
			code, message = "textedit_failed", "selected text processing failed"
		}
		if classified, ok := ux.Classify(err); ok {
			message = classified
		}
		return failure(code, message)
	}
	return success(outcome)
}

func failure(code, message string) response {
	return response{Error: &bridgeError{Code: code, Message: message}}
}

func success(value any) response {
	return response{OK: true, Result: value}
}

// Physical Quartz codes are a Mac host fact, not a second voice rule.
func macKeyCode(s string) int {
	return map[string]int{macDefaultDictation: 54, macDefaultRewrite: 61, "Mouse4": 103, "Mouse5": 104}[s]
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
	if errDictation != nil || errRewrite != nil || macKeyCode(dictation) == 0 || macKeyCode(rewrite) == 0 || dictation == rewrite {
		return failure("invalid_config", msgInvalidConfig)
	}
	cfg.DictationHotkey, cfg.HotkeyRecord = dictation, rewrite
	cfg, err = coreconfig.Normalize(cfg)
	if err != nil || cfg.RecordMode != "hold" || cfg.AudioDeviceID != -1 {
		return failure("invalid_config", msgInvalidConfig)
	}
	reply := success(map[string]any{"profile": map[string]any{
		"dictation": cfg.DictationHotkey, "rewrite": cfg.HotkeyRecord,
		"maximumSeconds": cfg.MaxRecordSeconds, "modifierDelayMS": 200,
		"pasteRestoreMS": ux.PasteReceiptMinimumMilliseconds,
		"mouseDelayMS":   int(input.WindowsSideHold / time.Millisecond),
		"pill":           indicator.HostSpec(),
	}})
	reply.cfg = cfg
	return reply
}

// Required by Go's c-archive build; the native host calls VRRun directly.
func main() {}
