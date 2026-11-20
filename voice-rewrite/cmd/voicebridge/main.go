// voicebridge provides the linked macOS Go Core and an optional serial JSONL diagnostic command.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
	"unicode/utf8"

	"voicerewrite/internal/core/audio"
	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/indicator"
	"voicerewrite/internal/core/input"
	"voicerewrite/internal/core/jsonl"
	"voicerewrite/internal/core/speech"
	"voicerewrite/internal/core/textedit"
	"voicerewrite/internal/core/ux"
)

const (
	maxPCMBytes      = audio.PCMBytesPerSecond * coreconfig.RecordSecondsMax
	maxSelectedBytes = 1 << 20
	msgUnsupported   = "unsupported operation"
	msgCanceled      = "request canceled"
	msgTimedOut      = "request timed out"
	msgInvalidConfig = "invalid configuration"
)

var errFrameTooLarge = errors.New("frame exceeds limit")

type request struct {
	Version      int             `json:"v"`
	ID           string          `json:"id"`
	Op           string          `json:"op"`
	Config       json.RawMessage `json:"config"`
	PCMBase64    string          `json:"pcm_base64"`
	SelectedText string          `json:"selected_text"`
	CanInsert    bool            `json:"can_insert"`
	Instruction  string          `json:"instruction"`
	NativePCM    []byte          `json:"-"`
}

type bridgeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type response struct {
	Version int          `json:"v"`
	ID      string       `json:"id"`
	OK      bool         `json:"ok"`
	Result  any          `json:"result,omitempty"`
	Error   *bridgeError `json:"error,omitempty"`
}

type bridgeOptions struct {
	TranscribeTimeout     time.Duration
	SpokenSelectedTimeout time.Duration
}

func defaultOptions() bridgeOptions {
	return bridgeOptions{TranscribeTimeout: speech.TranscribeTimeout, SpokenSelectedTimeout: textedit.SpokenTaskTimeout}
}

// Short clips should not wait for a stalled service as long as a multi-chunk
// recording. This budget is shared by JSONL and native PCM: 65..220 seconds.
func transcribeBudget(pcmBytes int) time.Duration {
	if pcmBytes < 0 {
		pcmBytes = 0
	}
	seconds := (pcmBytes + audio.PCMBytesPerSecond - 1) / audio.PCMBytesPerSecond
	budget := time.Duration(45+2*seconds) * time.Second
	if budget < 65*time.Second {
		return 65 * time.Second
	}
	if budget > speech.TranscribeTimeout {
		return speech.TranscribeTimeout
	}
	return budget
}

func (o bridgeOptions) timeoutFor(req request) time.Duration {
	if req.Op == "selected_voice_task" {
		return o.SpokenSelectedTimeout
	}
	pcmBytes := len(req.NativePCM)
	if req.NativePCM == nil {
		// The JSONL protocol carries base64; calculate the original byte
		// estimate without allocating or decoding a second copy.
		pcmBytes = len(req.PCMBase64) / 4 * 3
		if n := len(req.PCMBase64); n > 0 && req.PCMBase64[n-1] == '=' {
			pcmBytes--
			if n > 1 && req.PCMBase64[n-2] == '=' {
				pcmBytes--
			}
		}
	}
	if budget := transcribeBudget(pcmBytes); budget < o.TranscribeTimeout {
		return budget
	}
	return o.TranscribeTimeout
}

// Both the JSONL service and in-process macOS C ABI use this boundary.
// It owns the final cancellation result so ASR and rewrite do not each
// duplicate context checks; private provider errors and panics stay contained.
func safeProcessRequest(ctx context.Context, req request) (reply response) {
	reply = failure(req.ID, "internal_error", "request failed")
	defer func() { _ = recover() }()
	reply = processRequest(ctx, req)
	if err := ctx.Err(); err != nil {
		return contextFailure(req.ID, err)
	}
	return reply
}

func failure(id, code, message string) response {
	return response{Version: jsonl.Version, ID: id, Error: &bridgeError{Code: code, Message: message}}
}

func success(id string, value any) response {
	return response{Version: jsonl.Version, ID: id, OK: true, Result: value}
}

// readFrame bounds memory even if a client sends a line without a terminator.
// On an oversized line, consume through its delimiter before accepting another.
func readFrame(r *bufio.Reader) ([]byte, error) {
	var frame []byte
	oversized := false
	for {
		chunk, err := r.ReadSlice('\n')
		if !oversized {
			if len(frame)+len(chunk) > jsonl.MaxRequestBytes {
				frame = nil
				oversized = true
			} else {
				frame = append(frame, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if oversized {
			return nil, errFrameTooLarge
		}
		if err == io.EOF {
			if len(frame) == 0 {
				return nil, io.EOF
			}
			return frame, nil
		}
		if err != nil {
			return nil, err
		}
		return bytes.TrimSuffix(frame, []byte{'\n'}), nil
	}
}

// The shipped macOS app calls Go in-process. The optional JSONL command is
// deliberately serial; it does not need an active-task map or a second cancel
// protocol. Timeouts still bound every ASR/LLM operation.
func dispatch(frame []byte, options bridgeOptions) response {
	var req request
	if !utf8.Valid(frame) || json.Unmarshal(frame, &req) != nil {
		return failure("", "invalid_json", "invalid request JSON")
	}
	if req.Version != jsonl.Version {
		return failure(req.ID, "protocol_version", "unsupported protocol version")
	}
	if req.ID == "" || len(req.ID) > jsonl.MaxIDBytes {
		return failure(req.ID, "invalid_id", fmt.Sprintf("request ID must be 1..%d bytes", jsonl.MaxIDBytes))
	}
	switch req.Op {
	case "hello":
		return success(req.ID, map[string]any{"name": jsonl.HelloName, "protocol": jsonl.Version})
	case "host_profile":
		return headlessProfile(req)
	case "transcribe", "selected_voice_task":
		ctx, cancel := context.WithTimeout(context.Background(), options.timeoutFor(req))
		defer cancel()
		return safeProcessRequest(ctx, req)
	default:
		return failure(req.ID, "unsupported_op", msgUnsupported)
	}
}

// headlessProfile shares the existing config normalizer and indicator with
// Windows; only physical Mac shortcut identifiers differ.
func headlessProfile(req request) response {
	var provided map[string]json.RawMessage
	if len(req.Config) == 0 || json.Unmarshal(req.Config, &provided) != nil || provided == nil {
		return failure(req.ID, "invalid_config", msgInvalidConfig)
	}
	cfg, err := coreconfig.Parse(req.Config)
	if err != nil {
		return failure(req.ID, "invalid_config", msgInvalidConfig)
	}
	if _, ok := provided["dictation_hotkey"]; !ok {
		cfg.DictationHotkey = "RightCommand"
	}
	if _, ok := provided["hotkey_record"]; !ok {
		cfg.HotkeyRecord = "RightOption"
	}
	allowed := func(s string) bool {
		return s == "RightCommand" || s == "RightOption" || s == "Mouse4" || s == "Mouse5"
	}
	if !allowed(cfg.DictationHotkey) || !allowed(cfg.HotkeyRecord) ||
		cfg.DictationHotkey == cfg.HotkeyRecord ||
		cfg.MaxRecordSeconds < coreconfig.RecordSecondsMin ||
		cfg.MaxRecordSeconds > coreconfig.RecordSecondsMax {
		return failure(req.ID, "invalid_config", msgInvalidConfig)
	}
	cfg, err = coreconfig.Normalize(cfg)
	if err != nil {
		return failure(req.ID, "invalid_config", msgInvalidConfig)
	}
	return success(req.ID, map[string]any{"profile": map[string]any{
		"dictation": cfg.DictationHotkey, "rewrite": cfg.HotkeyRecord,
		"maximumSeconds": cfg.MaxRecordSeconds, "modifierDelayMS": 200,
		"pasteRestoreMS": ux.PasteReceiptMinimumMilliseconds,
		"mouseDelayMS":   int(input.WindowsSideHold / time.Millisecond),
		"pill":           indicator.HostSpec(),
	}})
}

func decodeRequestPCM(req request, cfg coreconfig.Config) ([]byte, string) {
	maxBytes := maxPCMBytes
	if limit := cfg.MaxRecordSeconds * audio.PCMBytesPerSecond; limit > 0 && limit < maxBytes {
		maxBytes = limit
	}
	var pcm []byte
	if req.NativePCM != nil {
		pcm = req.NativePCM
	} else {
		if len(req.PCMBase64) == 0 || len(req.PCMBase64) > base64.StdEncoding.EncodedLen(maxBytes) {
			return nil, "invalid"
		}
		var err error
		pcm, err = base64.StdEncoding.Strict().DecodeString(req.PCMBase64)
		if err != nil {
			return nil, "invalid"
		}
	}
	if len(pcm) == 0 || len(pcm)%2 != 0 || len(pcm) > maxBytes {
		return nil, "invalid"
	}
	if audio.BelowMinCapture(pcm) {
		return nil, "short"
	}
	return pcm, ""
}

func rejectPCM(id, op, reason string) response {
	if reason == "short" {
		message := ux.DictationTooShort
		if op == "selected_voice_task" {
			message = ux.RewriteTooShort
		}
		return failure(id, "invalid_pcm", message)
	}
	return failure(id, "invalid_pcm", "PCM must be 16 kHz mono PCM16, at least 0.1s and within duration limit")
}

func processRequest(ctx context.Context, req request) response {
	trimmed := bytes.TrimSpace(req.Config)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return failure(req.ID, "invalid_config", "config object is required")
	}
	if trimmed[0] != '{' {
		return failure(req.ID, "invalid_config", "config must be an object")
	}
	cfg, err := coreconfig.Parse(req.Config)
	if err != nil {
		return failure(req.ID, "invalid_config", msgInvalidConfig)
	}
	cfg, err = coreconfig.Normalize(cfg)
	if err != nil {
		return failure(req.ID, "invalid_config", msgInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return contextFailure(req.ID, err)
	}
	switch req.Op {
	case "transcribe":
		pcm, reason := decodeRequestPCM(req, cfg)
		if reason != "" {
			return rejectPCM(req.ID, req.Op, reason)
		}
		text, err := speech.Transcribe(ctx, cfg, pcm)
		if err != nil {
			return providerFailure(req.ID, "asr_failed", "speech transcription failed", err)
		}
		route := "preview"
		if req.CanInsert {
			route = "insert"
		}
		return success(req.ID, map[string]any{"text": text, "route": route})
	case "selected_voice_task":
		if len(req.SelectedText) == 0 || len(req.SelectedText) > maxSelectedBytes ||
			!utf8.ValidString(req.SelectedText) || req.Instruction != "" {
			return failure(req.ID, "invalid_request", "selected text or instruction exceeds limits")
		}
		pcm, reason := decodeRequestPCM(req, cfg)
		if reason != "" {
			return rejectPCM(req.ID, req.Op, reason)
		}
		result, instruction, err := textedit.ResolveSpokenTask(ctx, cfg, req.SelectedText, pcm, nil)
		if err != nil {
			return providerFailure(req.ID, "textedit_failed", "selected text processing failed", err)
		}
		action := textedit.RouteTask(result, instruction)
		route := "preview"
		if action == textedit.Replace {
			route = "replace"
		}
		return success(req.ID, map[string]any{
			"intent": result.Intent, "text": result.Result,
			"route": route, "instruction": instruction,
		})
	}
	return failure(req.ID, "unsupported_op", msgUnsupported)
}

// Classified recovery text is a fixed catalog. Anything else stays a constant
// so provider bodies, keys, and selected text never reach the protocol.
func providerFailure(id, code, fallback string, err error) response {
	if message, ok := ux.Classify(err); ok {
		return failure(id, code, message)
	}
	return failure(id, code, fallback)
}

func contextFailure(id string, err error) response {
	if errors.Is(err, context.DeadlineExceeded) {
		return failure(id, "timeout", msgTimedOut)
	}
	return failure(id, "canceled", msgCanceled)
}

// serveWithOptions is a serial diagnostic endpoint. Its one-request-then-EOF
// lifecycle is tested, but production macOS calls safeProcessRequest directly
// through the C ABI rather than spawning this command.
func serveWithOptions(input io.Reader, output io.Writer, options bridgeOptions) error {
	if options.TranscribeTimeout <= 0 || options.SpokenSelectedTimeout <= 0 {
		return errors.New("invalid bridge options")
	}
	reader := bufio.NewReaderSize(input, 64<<10)
	encoder := json.NewEncoder(output)
	for {
		frame, err := readFrame(reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		var reply response
		switch {
		case errors.Is(err, errFrameTooLarge):
			reply = failure("", "request_too_large", fmt.Sprintf("request exceeds %d MiB", jsonl.MaxRequestBytes>>20))
		case err != nil:
			return err
		default:
			reply = dispatch(frame, options)
		}
		if err := encoder.Encode(reply); err != nil {
			return err
		}
	}
}

func serve(input io.Reader, output io.Writer) error {
	return serveWithOptions(input, output, defaultOptions())
}

func main() {
	if err := serve(os.Stdin, os.Stdout); err != nil {
		// No request data, keys, URLs or selected text are written to stderr.
		if !errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stderr, "voicebridge: input/output failure")
		}
		os.Exit(1)
	}
}
