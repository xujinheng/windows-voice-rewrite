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
	"net/http"
	"os"
	"sync"
	"time"
	"unicode/utf8"

	"voicerewrite/internal/core/audio"
	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/jsonl"
	meetingcore "voicerewrite/internal/core/meeting"
	"voicerewrite/internal/core/provider"
	"voicerewrite/internal/core/serviceprobe"
	"voicerewrite/internal/core/shortcut"
	sessioncore "voicerewrite/internal/core/session"
	"voicerewrite/internal/core/speech"
	"voicerewrite/internal/core/textedit"
	"voicerewrite/internal/core/update"
	"voicerewrite/internal/core/ux"
)

const (
	maxPCMBytes         = audio.PCMBytesPerSecond * coreconfig.RecordSecondsMax
	maxSelectedBytes    = 1 << 20
	maxInstructionBytes = 32 << 10
	msgUnsupported      = "unsupported operation"
	msgCanceled         = "request canceled"
	msgTimedOut         = "request timed out"
	msgInvalidConfig    = "invalid configuration"
)

var errFrameTooLarge = errors.New("frame exceeds limit")

type request struct {
	Version         int             `json:"v"`
	ID              string          `json:"id"`
	Op              string          `json:"op"`
	Config          json.RawMessage `json:"config"`
	PCMBase64       string          `json:"pcm_base64"`
	SelectedText    string          `json:"selected_text"`
	CanInsert       bool            `json:"can_insert"`
	Instruction     string          `json:"instruction"`
	CurrentVersion  string          `json:"current_version"`
	Shortcut        string          `json:"shortcut"`
	TargetID        string          `json:"target_id"`
	SessionID       string          `json:"session_id"`
	AudioEpoch      uint64          `json:"audio_epoch"`
	ReplyIndex      int             `json:"reply_index"`
	ViewButton      int             `json:"button_id"`
	FocusExpression bool            `json:"focus_expression"`
	EpochEvent      string          `json:"epoch_event"`
	Generation      uint64          `json:"generation"`
	Stage           string          `json:"stage"`
	RequestID       string          `json:"request_id"`
	Reason          string          `json:"reason"`
	AudioBytes      int             `json:"audio_bytes"`
	RecordSeconds   int             `json:"record_seconds"`
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
	SelectedTimeout       time.Duration
	SpokenSelectedTimeout time.Duration
}

func defaultOptions() bridgeOptions {
	return bridgeOptions{MaxInFlight: 4, TranscribeTimeout: 220 * time.Second, SelectedTimeout: 150 * time.Second, SpokenSelectedTimeout: 360 * time.Second}
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
	meeting *bridgeMeeting
	voiceEpoch sessioncore.Policy

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
		if s.meeting != nil {
			s.meeting.stop()
			s.meeting = nil
		}
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

	if req.Op != "transcribe" && req.Op != "selected_task" && req.Op != "selected_voice_task" && req.Op != "probe_asr" && req.Op != "probe_llm" && req.Op != "list_asr_models" && req.Op != "list_llm_models" && req.Op != "check_update" && req.Op != "meeting_replies" && req.Op != "meeting_refine" && req.Op != "meeting_compose" && req.Op != "meeting_express" {
		s.mu.Unlock()
		switch req.Op {
		case "meeting_start", "meeting_feed", "meeting_snapshot", "meeting_stop", "meeting_pause", "meeting_resume", "meeting_accept", "meeting_cancel", "meeting_view", "meeting_configure", "meeting_demo":
			s.send(s.meetingRequest(req))
		case "voice_epoch":
			s.send(s.voiceEpochRequest(req))
		case "hello":
			s.send(success(req.ID, map[string]any{"name": jsonl.HelloName, "protocol": jsonl.Version}))
		case "parse_shortcut":
			s.send(parseShortcut(req))
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
	if req.Op == "check_update" {
		timeout = 8 * time.Second
	} else if req.Op == "list_asr_models" || req.Op == "list_llm_models" {
		timeout = 12 * time.Second
	}
	if req.Op == "meeting_replies" || req.Op == "meeting_refine" {
		timeout = 40 * time.Second
	}
	if req.Op == "meeting_compose" || req.Op == "meeting_express" {
		timeout = 60 * time.Second
	}
	if req.Op == "selected_task" {
		timeout = s.options.SelectedTimeout
	} else if req.Op == "selected_voice_task" {
		timeout = s.options.SpokenSelectedTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	task := &bridgeTask{cancel: cancel}
	s.active[req.ID] = task
	s.wg.Add(1)
	s.mu.Unlock()
	go s.runTask(ctx, task, req)
}

// voice_epoch is a lightweight control event, never a per-audio-frame RPC.
// The same session.Policy code is consumed by the Windows worker. Each reply
// echoes exact generation ownership and the Go-issued effects.
func (s *bridgeServer) voiceEpochRequest(req request) response {
    decision := s.voiceEpoch.Apply(sessioncore.Event{
        Type: sessioncore.EventType(req.EpochEvent),
        SessionID: req.SessionID,
        Generation: req.Generation,
        Stage: sessioncore.Stage(req.Stage),
        RequestID: req.RequestID,
        Reason: sessioncore.Reason(req.Reason),
        AudioBytes: req.AudioBytes,
        RecordSeconds: req.RecordSeconds,
    })
    effects := make([]map[string]any, 0, len(decision.Effects))
    for _, effect := range decision.Effects {
        effects = append(effects, map[string]any{
            "id":effect.ID, "type":effect.Type,
            "session_id":effect.SessionID, "generation":effect.Generation,
            "stage":effect.Stage, "reason":effect.Reason, "request_id":effect.RequestID,
        })
        if effect.Type == "cancel_request" && effect.RequestID != "" {
            s.mu.Lock()
            if active := s.active[effect.RequestID]; active != nil {
                active.canceled = true
                active.cancel()
            }
            s.mu.Unlock()
        }
    }
    return success(req.ID, map[string]any{
        "allowed":decision.Allowed, "session_id":decision.SessionID,
        "generation":decision.Generation, "stage":decision.Stage,
        "reason":decision.Reason, "budget_ms":decision.BudgetMilliseconds,
        "effects":effects,
    })
}

func (s *bridgeServer) meetingRequest(req request) response {
	switch req.Op {
	case "meeting_demo":
		// Fixture-only: no session, audio capture, ASR or LLM connection.
		return success(req.ID, meetingDemoSnapshot())
	case "meeting_start":
		if len(req.Config) == 0 {
			return failure(req.ID, "invalid_config", msgInvalidConfig)
		}
		cfg, err := coreconfig.Parse(req.Config)
		if err != nil {
			return failure(req.ID, "invalid_config", msgInvalidConfig)
		}
		cfg, err = coreconfig.Normalize(cfg)
		if err != nil {
			return failure(req.ID, "invalid_config", msgInvalidConfig)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.meeting != nil {
			return failure(req.ID, "meeting_active", "请先停止当前会议采集")
		}
		m, err := newBridgeMeeting(cfg)
		if err != nil {
			return failure(req.ID, "meeting_failed", "无法启动会议处理")
		}
		s.meeting = m
		_, epoch, _ := m.runtime.Snapshot()
		return success(req.ID, map[string]any{"session_id": m.id, "audio_epoch": epoch})
	case "meeting_feed":
		if req.SessionID == "" || len(req.PCMBase64) == 0 ||
			len(req.PCMBase64) > base64.StdEncoding.EncodedLen(32_000) {
			return failure(req.ID, "invalid_pcm", "会议音频帧大小无效")
		}
		pcm, err := base64.StdEncoding.Strict().DecodeString(req.PCMBase64)
		if err != nil || len(pcm) == 0 || len(pcm) > 32_000 || len(pcm)%audio.FrameBytes != 0 {
			return failure(req.ID, "invalid_pcm", "会议音频必须是 20ms 对齐的 16kHz 单声道 PCM16")
		}
		s.mu.Lock()
		m := s.meeting
		s.mu.Unlock()
		if m == nil || m.id != req.SessionID {
			return failure(req.ID, "stale_meeting", "会议已停止或更换")
		}
		if err := m.feed(pcm, req.AudioEpoch); err != nil {
			switch {
			case errors.Is(err, errMeetingPaused):
				return success(req.ID, map[string]any{"accepted": false, "paused": true})
			case errors.Is(err, errMeetingStaleEpoch):
				return success(req.ID, map[string]any{"accepted": false, "stale_epoch": true})
			case errors.Is(err, context.Canceled):
				return failure(req.ID, "stale_meeting", "会议已停止或更换")
			default:
				return failure(req.ID, "meeting_busy", "会议音频队列已满，已停止接收当前音频帧")
			}
		}
		return success(req.ID, map[string]any{"accepted": true})
	case "meeting_snapshot", "meeting_stop", "meeting_pause", "meeting_resume", "meeting_accept", "meeting_cancel", "meeting_view", "meeting_configure":
		s.mu.Lock()
		m := s.meeting
		if m == nil || req.SessionID == "" || req.SessionID != m.id {
			s.mu.Unlock()
			return failure(req.ID, "stale_meeting", "会议已停止或更换")
		}
		if req.Op == "meeting_stop" {
			s.meeting = nil
			m.stop()
			s.mu.Unlock()
			return success(req.ID, map[string]any{"stopped": true})
		}
		s.mu.Unlock()
		if req.Op == "meeting_configure" {
			if len(req.Config) == 0 {
				return failure(req.ID, "invalid_config", msgInvalidConfig)
			}
			cfg, err := coreconfig.Parse(req.Config)
			if err != nil {
				return failure(req.ID, "invalid_config", msgInvalidConfig)
			}
			cfg, err = coreconfig.Normalize(cfg)
			if err != nil {
				return failure(req.ID, "invalid_config", msgInvalidConfig)
			}
			if err := m.reconfigure(cfg); err != nil {
				return failure(req.ID, "invalid_profile", "会议资料或识别模式无效；请检查内容长度和模式")
			}
		}
		if req.Op == "meeting_pause" || req.Op == "meeting_resume" {
			if err := m.setPaused(req.Op == "meeting_pause"); err != nil {
				return failure(req.ID, "stale_meeting", "会议已停止或更换")
			}
		}
		if req.Op == "meeting_accept" {
			if !m.acceptReplies() {
				return failure(req.ID, "no_pending_reply", "没有可切换的新建议")
			}
		}
		if req.Op == "meeting_view" {
			if req.FocusExpression {
				m.focusExpression()
			} else if !m.applyViewButton(req.ViewButton) {
				return failure(req.ID, "invalid_request", "不支持的会议视图操作")
			}
		}
		if req.Op == "meeting_cancel" {
			m.cancelWork()
		}
		return success(req.ID, m.snapshot())
	}
	return failure(req.ID, "unsupported_op", msgUnsupported)
}

func (s *bridgeServer) runTask(ctx context.Context, task *bridgeTask, req request) {
	defer s.wg.Done()
	defer task.cancel()

	reply := failure(req.ID, "internal_error", "request failed")
	func() {
		// Provider errors and unexpected panics may contain private request data.
		// Reaching the protocol requires a fixed recovery string or a constant fallback.
		defer func() { _ = recover() }()
		if req.Op == "meeting_replies" || req.Op == "meeting_refine" || req.Op == "meeting_compose" || req.Op == "meeting_express" {
			reply = s.meetingTask(ctx, req)
		} else {
			reply = processRequest(ctx, req)
		}
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

func (s *bridgeServer) meetingTask(ctx context.Context, req request) response {
	s.mu.Lock()
	m := s.meeting
	s.mu.Unlock()
	if m == nil || req.SessionID == "" || req.SessionID != m.id {
		return failure(req.ID, "stale_meeting", "会议已停止或更换")
	}
	if len(req.Instruction) > 8<<10 || !utf8.ValidString(req.Instruction) {
		return failure(req.ID, "invalid_request", "会议指令超出长度限制")
	}
	var err error
	switch req.Op {
	case "meeting_replies":
		err = m.runReplies(ctx, 0)
	case "meeting_refine":
		if _, ok := meetingcore.AcceptRefine(req.ReplyIndex, req.Instruction); !ok {
			return failure(req.ID, "invalid_request", "请选择回复并输入 600 字以内的调整指令")
		}
		err = m.refine(ctx, req.ReplyIndex, req.Instruction)
	case "meeting_express":
		if !meetingcore.ValidInstruction(req.Instruction) {
			return failure(req.ID, "invalid_request", "请输入 2000 字以内需要忠实翻译的内容")
		}
		err = m.express(ctx, req.Instruction)
	case "meeting_compose":
		if !meetingcore.ValidInstruction(req.Instruction) {
			return failure(req.ID, "invalid_request", "请输入 2000 字以内想表达的内容")
		}
		err = m.compose(ctx, req.Instruction)
	}
	if ctx.Err() != nil {
		return contextFailure(req.ID, ctx.Err())
	}
	if m.runtime.Context().Err() != nil {
		return failure(req.ID, "stale_meeting", "会议已停止或更换")
	}
	if err != nil {
		switch {
		case errors.Is(err, errMeetingNoFinalTurn):
			return failure(req.ID, "invalid_request", meetingcore.RepliesWait)
		case errors.Is(err, errMeetingInvalidReply):
			return failure(req.ID, "invalid_request", "请选择一条完整英文建议后再微调")
		case errors.Is(err, context.Canceled), errors.Is(err, meetingcore.ErrPreempted):
			return failure(req.ID, "canceled", msgCanceled)
		case errors.Is(err, context.DeadlineExceeded):
			return failure(req.ID, "timeout", msgTimedOut)
		default:
			return providerFailure(req.ID, "meeting_inference_failed", "会议英文生成失败；请检查 LLM 服务后重试", err)
		}
	}
	return success(req.ID, m.snapshot())
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

func selectedTaskSuccess(id string, result textedit.AssistantResult, instruction string, spoken bool) response {
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
	if spoken {
		payload["instruction"] = instruction
	}
	return success(id, payload)
}

func parseShortcut(req request) response {
	binding, err := shortcut.Bind(req.Shortcut)
	if err != nil {
		return failure(req.ID, "invalid_shortcut", err.Error())
	}
	return success(req.ID, map[string]any{
		"normalized": binding.Normalized,
		"key":        binding.Key,
		"mouse":      binding.Mouse,
		"ctrl":       binding.Ctrl,
		"alt":        binding.Alt,
		"shift":      binding.Shift,
	})
}

func processRequest(ctx context.Context, req request) response {
	if req.Op == "check_update" {
		notice := update.Fetch(ctx, req.CurrentVersion, &http.Client{
			Timeout:       8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		})
		if notice.Err != nil {
			return failure(req.ID, "update_failed", notice.Summary(req.CurrentVersion))
		}
		return success(req.ID, map[string]any{
			"found": notice.Found, "newer": notice.Newer, "latest": notice.Latest,
			"page": update.ReleasesPage, "summary": notice.Summary(req.CurrentVersion),
		})
	}
	if len(req.Config) == 0 || bytes.Equal(bytes.TrimSpace(req.Config), []byte("null")) {
		return failure(req.ID, "invalid_config", "config object is required")
	}
	if len(bytes.TrimSpace(req.Config)) == 0 || bytes.TrimSpace(req.Config)[0] != '{' {
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
	case "list_asr_models", "list_llm_models":
		base, key := cfg.ASRBaseURL, cfg.ASRAPIKey
		if req.Op == "list_llm_models" {
			base, key = cfg.LLMBaseURL, cfg.LLMAPIKey
		}
		models, err := provider.ListModels(ctx, base, key)
		if err != nil {
			if ctx.Err() != nil {
				return contextFailure(req.ID, ctx.Err())
			}
			return providerFailure(req.ID, "model_list_failed", "无法读取模型列表；请检查服务地址和 API Key", err)
		}
		truncated := len(models) > 100
		if truncated {
			models = models[:100]
		}
		return success(req.ID, map[string]any{"models": models, "truncated": truncated})
	case "probe_asr":
		message := serviceprobe.ProbeModel(ctx, "ASR", cfg.ASRBaseURL, cfg.ASRAPIKey, cfg.ASRModel)
		return success(req.ID, map[string]string{"kind": "ASR", "message": message})
	case "probe_llm":
		message := serviceprobe.ProbeModel(ctx, "LLM", cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.TextModel)
		return success(req.ID, map[string]string{"kind": "LLM", "message": message})
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
	case "selected_task", "selected_voice_task":
		if len(req.SelectedText) == 0 || len(req.SelectedText) > maxSelectedBytes ||
			!utf8.ValidString(req.SelectedText) || len(req.Instruction) > maxInstructionBytes ||
			!utf8.ValidString(req.Instruction) {
			return failure(req.ID, "invalid_request", "selected text or instruction exceeds limits")
		}
		var result textedit.AssistantResult
		var instruction string
		if req.Op == "selected_voice_task" {
			if req.Instruction != "" {
				return failure(req.ID, "invalid_request", "spoken selected text task must not supply a text instruction")
			}
			pcm, reason := decodeRequestPCM(req, cfg)
			if reason != "" {
				return rejectPCM(req.ID, req.Op, reason)
			}
			result, instruction, err = textedit.ResolveSpokenTask(ctx, cfg, req.SelectedText, pcm, nil)
		} else {
			instruction = req.Instruction
			result, err = textedit.ResolveSelectedTask(ctx, cfg, req.SelectedText, instruction)
		}
		if err != nil {
			if ctx.Err() != nil {
				return contextFailure(req.ID, ctx.Err())
			}
			return providerFailure(req.ID, "textedit_failed", "selected text processing failed", err)
		}
		if ctx.Err() != nil {
			return contextFailure(req.ID, ctx.Err())
		}
		return selectedTaskSuccess(req.ID, result, instruction, req.Op == "selected_voice_task")
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
	if options.MaxInFlight < 1 || options.TranscribeTimeout <= 0 || options.SelectedTimeout <= 0 || options.SpokenSelectedTimeout <= 0 {
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
	s.shutdown()
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
