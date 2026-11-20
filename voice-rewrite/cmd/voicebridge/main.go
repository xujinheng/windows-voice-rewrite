// voicebridge links the shared Go task directly into the macOS native process.
package main

import (
	"bytes"
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
	maxPCMBytes      = audio.PCMBytesPerSecond * coreconfig.RecordSecondsMax
	maxSelectedBytes = 1 << 20
	msgCanceled      = "request canceled"
	msgTimedOut      = "request timed out"
	msgInvalidConfig = "invalid configuration"
)

type request struct {
	Rewrite      bool
	Config       json.RawMessage
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
}

// The native C ABI uses this boundary.
// It owns the final cancellation result so ASR and rewrite do not each
// duplicate context checks; private provider errors and panics stay contained.
func safeProcessRequest(ctx context.Context, req request) (reply response) {
	reply = failure("internal_error", "request failed")
	defer func() { _ = recover() }()
	reply = processRequest(ctx, req)
	if err := ctx.Err(); err != nil {
		return contextFailure(err)
	}
	return reply
}

func failure(code, message string) response {
	return response{Error: &bridgeError{Code: code, Message: message}}
}

func success(value any) response {
	return response{OK: true, Result: value}
}

// headlessProfile shares the existing config normalizer and indicator with
// Windows; only physical Mac shortcut identifiers differ.
func headlessProfile(req request) response {
	var provided map[string]json.RawMessage
	if len(req.Config) == 0 || json.Unmarshal(req.Config, &provided) != nil || provided == nil {
		return failure("invalid_config", msgInvalidConfig)
	}
	cfg, err := coreconfig.Parse(req.Config)
	if err != nil {
		return failure("invalid_config", msgInvalidConfig)
	}
	if _, ok := provided["dictation_hotkey"]; !ok {
		cfg.DictationHotkey = shortcut.MacDictation
	}
	if _, ok := provided["hotkey_record"]; !ok {
		cfg.HotkeyRecord = shortcut.MacRewrite
	}
	dictation, _, _, _, errDictation := shortcut.Parse(cfg.DictationHotkey)
	rewrite, _, _, _, errRewrite := shortcut.Parse(cfg.HotkeyRecord)
	if errDictation != nil || errRewrite != nil || shortcut.MacKeyCode(dictation) == 0 || shortcut.MacKeyCode(rewrite) == 0 || dictation == rewrite {
		return failure("invalid_config", msgInvalidConfig)
	}
	cfg.DictationHotkey, cfg.HotkeyRecord = dictation, rewrite
	cfg, err = coreconfig.Normalize(cfg)
	if err != nil || cfg.RecordMode != "hold" || cfg.AudioDeviceID != -1 {
		return failure("invalid_config", msgInvalidConfig)
	}
	return success(map[string]any{"profile": map[string]any{
		"dictation": cfg.DictationHotkey, "rewrite": cfg.HotkeyRecord,
		"maximumSeconds": cfg.MaxRecordSeconds, "modifierDelayMS": 200,
		"pasteRestoreMS": ux.PasteReceiptMinimumMilliseconds,
		"mouseDelayMS":   int(input.WindowsSideHold / time.Millisecond),
		"pill":           indicator.HostSpec(),
	}})
}

func processRequest(ctx context.Context, req request) response {
	trimmed := bytes.TrimSpace(req.Config)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return failure("invalid_config", "config object is required")
	}
	if trimmed[0] != '{' {
		return failure("invalid_config", "config must be an object")
	}
	cfg, err := coreconfig.Parse(req.Config)
	if err != nil {
		return failure("invalid_config", msgInvalidConfig)
	}
	cfg, err = coreconfig.Normalize(cfg)
	if err != nil {
		return failure("invalid_config", msgInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return contextFailure(err)
	}

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

func contextFailure(err error) response {
	if errors.Is(err, context.DeadlineExceeded) {
		return failure("timeout", msgTimedOut)
	}
	return failure("canceled", msgCanceled)
}

// Required by Go's c-archive build; the native host calls VRRun directly.
func main() {}
