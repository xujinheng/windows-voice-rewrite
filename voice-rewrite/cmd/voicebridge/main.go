// voicebridge is the local JSON-lines process entry point for the macOS host.
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
	"sync"
	"time"
	"unicode/utf8"

	"voicerewrite/internal/core/audio"
	coreconfig "voicerewrite/internal/core/config"
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
	TargetID     string          `json:"target_id"`
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
	MaxInFlight           int
	TranscribeTimeout     time.Duration
	SpokenSelectedTimeout time.Duration
}

func defaultOptions() bridgeOptions {
	return bridgeOptions{MaxInFlight: 4, TranscribeTimeout: 220 * time.Second, SpokenSelectedTimeout: 360 * time.Second}
}

// Short dictations should not remain in "processing" for the entire 220s
// multi-chunk maximum when a local or remote ASR service has stalled.
// Allow more time for longer audio; never extend an explicit test/host limit.
func transcribeBudget(base64Bytes int) time.Duration {
	if base64Bytes < 0 {
		base64Bytes = 0
	}
	// Base64 expands three PCM bytes to four characters, rounded upward.
	pcmBytes := base64Bytes / 4 * 3
	seconds := (pcmBytes + audio.PCMBytesPerSecond - 1) / audio.PCMBytesPerSecond
	budget := time.Duration(45+2*seconds) * time.Second
	if budget < 65*time.Second {
		return 65 * time.Second
	}
	if budget > 220*time.Second {
		return 220 * time.Second
	}
	return budget
}

type bridgeTask struct {
	cancel   context.CancelFunc
	canceled bool
}

type bridgeServer struct {
	mu      sync.Mutex
	active  map[string]*bridgeTask
	seen    map[string]struct{}
	wg      sync.WaitGroup
	options bridgeOptions

	input    io.Reader
	output   io.Writer
	writeMu  sync.Mutex
	writeErr error
	stopOnce sync.Once
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

func (s *bridgeServer) shutdown() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		for _, task := range s.active {
			task.canceled = true
			task.cancel()
		}
		s.mu.Unlock()
	})
}

// All writes, including asynchronous task completions, are one JSON line.
// A broken stdout cancels network work and closes stdin to unblock the reader.
func (s *bridgeServer) send(r response) {
	s.writeMu.Lock()
	if s.writeErr != nil {
		s.writeMu.Unlock()
		return
	}
	err := json.NewEncoder(s.output).Encode(r)
	if err != nil {
		s.writeErr = err
	}
	s.writeMu.Unlock()
	if err != nil {
		s.shutdown()
		if closer, ok := s.input.(io.Closer); ok {
			_ = closer.Close()
		}
	}
}

func (s *bridgeServer) outputError() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writeErr
}

func (s *bridgeServer) dispatch(frame []byte) {
	var req request
	if !utf8.Valid(frame) || json.Unmarshal(frame, &req) != nil {
		s.send(failure("", "invalid_json", "invalid request JSON"))
		return
	}
	if req.Version != jsonl.Version {
		s.send(failure(req.ID, "protocol_version", "unsupported protocol version"))
		return
	}
	if req.ID == "" || len(req.ID) > jsonl.MaxIDBytes {
		s.send(failure(req.ID, "invalid_id", fmt.Sprintf("request ID must be 1..%d bytes", jsonl.MaxIDBytes)))
		return
	}

	s.mu.Lock()
	if _, exists := s.seen[req.ID]; exists {
		s.mu.Unlock()
		s.send(failure(req.ID, "duplicate_id", "request ID already used"))
		return
	}
	if len(s.seen) >= jsonl.MaxSeenIDs {
		s.mu.Unlock()
		s.send(failure(req.ID, "id_limit", "helper request limit reached; restart helper"))
		return
	}
	s.seen[req.ID] = struct{}{}

	if req.Op != "transcribe" && req.Op != "selected_voice_task" {
		s.mu.Unlock()
		switch req.Op {
		case "hello":
			s.send(success(req.ID, map[string]any{"name": jsonl.HelloName, "protocol": jsonl.Version}))
		case "cancel":
			if req.TargetID == "" {
				s.send(failure(req.ID, "invalid_request", "target_id is required"))
				return
			}
			s.mu.Lock()
			task, found := s.active[req.TargetID]
			if found {
				task.canceled = true
				task.cancel()
			}
			s.mu.Unlock()
			s.send(success(req.ID, map[string]bool{"found": found}))
		default:
			s.send(failure(req.ID, "unsupported_op", msgUnsupported))
		}
		return
	}
	if len(s.active) >= s.options.MaxInFlight {
		s.mu.Unlock()
		s.send(failure(req.ID, "busy", "too many active requests"))
		return
	}
	timeout := s.options.TranscribeTimeout
	if req.Op == "transcribe" {
		if budget := transcribeBudget(len(req.PCMBase64)); budget < timeout {
			timeout = budget
		}
	}
	if req.Op == "selected_voice_task" {
		timeout = s.options.SpokenSelectedTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	task := &bridgeTask{cancel: cancel}
	s.active[req.ID] = task
	s.wg.Add(1)
	s.mu.Unlock()
	go s.runTask(ctx, task, req)
}

func (s *bridgeServer) runTask(ctx context.Context, task *bridgeTask, req request) {
	defer s.wg.Done()
	defer task.cancel()

	reply := failure(req.ID, "internal_error", "request failed")
	func() {
		// Provider errors and unexpected panics may contain private request data.
		// Reaching the protocol requires a fixed recovery string or a constant fallback.
		defer func() { _ = recover() }()
		reply = processRequest(ctx, req)
	}()

	s.mu.Lock()
	if task.canceled || errors.Is(ctx.Err(), context.Canceled) {
		reply = failure(req.ID, "canceled", msgCanceled)
	} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		reply = failure(req.ID, "timeout", msgTimedOut)
	}
	delete(s.active, req.ID)
	s.mu.Unlock()
	s.send(reply)
}

func decodeRequestPCM(req request, cfg coreconfig.Config) ([]byte, string) {
	maxBytes := maxPCMBytes
	if limit := cfg.MaxRecordSeconds * audio.PCMBytesPerSecond; limit > 0 && limit < maxBytes {
		maxBytes = limit
	}
	if len(req.PCMBase64) == 0 || len(req.PCMBase64) > base64.StdEncoding.EncodedLen(maxBytes) {
		return nil, "invalid"
	}
	pcm, err := base64.StdEncoding.Strict().DecodeString(req.PCMBase64)
	if err != nil || len(pcm) == 0 || len(pcm)%2 != 0 || len(pcm) > maxBytes {
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

func selectedTaskSuccess(id string, result textedit.AssistantResult, instruction string) response {
	action := textedit.RouteTask(result, instruction, false)
	route := "preview"
	if action == textedit.TryReplace || action == textedit.Replace {
		route = "replace"
	}
	payload := map[string]any{
		"intent": result.Intent,
		"text":   result.Result,
		"route":  route,
	}
	payload["instruction"] = instruction
	return success(id, payload)
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
			if ctx.Err() != nil {
				return contextFailure(req.ID, ctx.Err())
			}
			return providerFailure(req.ID, "asr_failed", "speech transcription failed", err)
		}
		if ctx.Err() != nil {
			return contextFailure(req.ID, ctx.Err())
		}
		return success(req.ID, map[string]any{
			"text":  text,
			"route": ux.RouteDictationTranscript(text, req.CanInsert),
		})
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
			if ctx.Err() != nil {
				return contextFailure(req.ID, ctx.Err())
			}
			return providerFailure(req.ID, "textedit_failed", "selected text processing failed", err)
		}
		if ctx.Err() != nil {
			return contextFailure(req.ID, ctx.Err())
		}
		return selectedTaskSuccess(req.ID, result, instruction)
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

func serveWithOptions(input io.Reader, output io.Writer, options bridgeOptions) error {
	if options.MaxInFlight < 1 || options.TranscribeTimeout <= 0 || options.SpokenSelectedTimeout <= 0 {
		return errors.New("invalid bridge options")
	}
	s := &bridgeServer{
		active: make(map[string]*bridgeTask), seen: make(map[string]struct{}),
		input: input, output: output, options: options,
	}
	reader := bufio.NewReaderSize(input, 64<<10)
	var readErr error
	for {
		frame, err := readFrame(reader)
		switch {
		case errors.Is(err, errFrameTooLarge):
			s.send(failure("", "request_too_large", fmt.Sprintf("request exceeds %d MiB", jsonl.MaxRequestBytes>>20)))
		case errors.Is(err, io.EOF):
			goto done
		case err != nil:
			readErr = err
			goto done
		default:
			s.dispatch(frame)
		}
		if s.outputError() != nil {
			break
		}
	}
done:
	// The headless host writes one request then closes stdin. EOF is not a
	// cancellation signal: let the bounded provider task return its reply.
	// Actual read failures and broken stdout still cancel in-flight work.
	if readErr != nil {
		s.shutdown()
	}
	s.wg.Wait()
	if err := s.outputError(); err != nil {
		return err
	}
	return readErr
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
