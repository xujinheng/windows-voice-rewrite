package session

import (
	"fmt"
	"sync"
	"time"
)

// Stage names are shared with macOS bridge events and Windows workers. Deadlines
// begin on entry to the named stage, not on the original key press.
type Stage string

const (
	StageMicrophoneStart   Stage = "microphone_start"
	StageRecording         Stage = "recording"
	StageCaptureStop       Stage = "capture_stop"
	StageDictationCapture  Stage = "dictation_capture"
	StageSelectionCapture  Stage = "selection_capture"
	StageASR               Stage = "asr"
	StageModel             Stage = "model"
	StageSpokenEdit        Stage = "spoken_edit"
	StageSelectedEdit      Stage = "selected_edit"
	StageNativeWrite       Stage = "native_write"
	StageDictationDelivery Stage = "dictation_delivery"
	StageSelectedWrite     Stage = "selected_write"
)

type Reason string

const (
	ReasonUser         Reason = "user"
	ReasonTimeout      Reason = "timeout"
	ReasonReplaced     Reason = "replaced"
	ReasonFocusChanged Reason = "focus_changed"
	ReasonNativeError  Reason = "native_error"
	ReasonRecordLimit  Reason = "record_limit"
)

type EventType string

const (
	EventBegin     EventType = "begin"
	EventStage     EventType = "stage"
	EventBind      EventType = "bind"
	EventAuthorize EventType = "authorize"
	EventReceipt   EventType = "receipt"
	EventCancel    EventType = "cancel"
	EventTimeout   EventType = "timeout"
	EventComplete  EventType = "complete"
)

type Event struct {
	Type          EventType
	SessionID     string
	Generation    uint64
	Stage         Stage
	RequestID     string
	Reason        Reason
	AudioBytes    int
	RecordSeconds int
}

type Effect struct {
	ID         string
	Type       string
	SessionID  string
	Generation uint64
	Stage      Stage
	Reason     Reason
	RequestID  string
}

type Decision struct {
	Allowed            bool
	SessionID          string
	Generation         uint64
	Stage              Stage
	Reason             Reason
	BudgetMilliseconds int64
	Effects            []Effect
}

type task struct {
	sessionID        string
	clientGeneration uint64
	epoch            uint64
	stage            Stage
	requestID        string
	finished         bool
}

// Policy is the same Go task/epoch/deadline authority for each native host.
// Each event is serialized; a late timeout, receipt or model reply cannot act on
// a new task even when the same native control has been reused.
type Policy struct {
	mu             sync.Mutex
	epoch          Epoch
	current        *task
	seen           map[string]struct{}
	lastGeneration uint64
	effectID       uint64
}

// CurrentEpoch returns the real Go-owned lease, never an inferred native
// generation. The caller must run ValidGeneration or CancelGeneration on this
// lease; a concurrent replacement revokes the returned token.
func (p *Policy) CurrentEpoch(sessionID string, generation uint64) (*Epoch, uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.current
	if t == nil || t.finished || t.sessionID != sessionID || t.clientGeneration != generation {
		return nil, 0
	}
	return &p.epoch, t.epoch
}

func StageBudget(stage Stage, audioBytes, recordSeconds int) time.Duration {
	switch stage {
	case StageMicrophoneStart:
		return 30 * time.Second
	case StageDictationCapture, StageCaptureStop, StageSelectionCapture,
		StageNativeWrite, StageDictationDelivery, StageSelectedWrite:
		return 20 * time.Second
	case StageASR:
		if audioBytes < 0 {
			audioBytes = 0
		}
		seconds := (audioBytes + 31999) / 32000
		timeout := 45 + 2*seconds
		if timeout < 65 {
			timeout = 65
		}
		if timeout > 220 {
			timeout = 220
		}
		return time.Duration(timeout+15) * time.Second
	case StageModel, StageSelectedEdit:
		return 165 * time.Second
	case StageSpokenEdit:
		return 375 * time.Second
	case StageRecording:
		if recordSeconds < 1 {
			recordSeconds = 180
		}
		if recordSeconds > 180 {
			recordSeconds = 180
		}
		return time.Duration(recordSeconds+2) * time.Second
	}
	return 0
}

func (p *Policy) effect(t *task, kind string, reason Reason) Effect {
	p.effectID++
	return Effect{
		ID:   fmt.Sprintf("%s/%d/%d", t.sessionID, t.clientGeneration, p.effectID),
		Type: kind, SessionID: t.sessionID, Generation: t.clientGeneration,
		Stage: t.stage, Reason: reason, RequestID: t.requestID,
	}
}

func (p *Policy) finish(t *task, reason Reason) []Effect {
	if t == nil || t.finished {
		return nil
	}
	t.finished = true
	p.epoch.Cancel(t.epoch, reason)
	effects := make([]Effect, 0, 4)
	if t.requestID != "" {
		effects = append(effects, p.effect(t, "cancel_request", reason))
	}
	effects = append(effects,
		p.effect(t, "stop_native_capture", reason),
		p.effect(t, "cancel_selection", reason),
		p.effect(t, "clear_task", reason))
	return effects
}

// Apply rejects unknown or repeated task IDs rather than recycling generation
// tokens. Results and receipts must present BOTH a native session ID and epoch.
func (p *Policy) Apply(e Event) Decision {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := Decision{SessionID: e.SessionID, Generation: e.Generation}
	if e.Type == EventBegin {
		if e.SessionID == "" || e.Generation == 0 || e.Generation <= p.lastGeneration {
			return result
		}
		if p.seen == nil {
			p.seen = make(map[string]struct{})
		}
		if _, used := p.seen[e.SessionID]; used {
			return result
		}
		result.Effects = p.finish(p.current, ReasonReplaced)
		p.lastGeneration = e.Generation
		// Generation never wraps, and UUID replay is rejected for the whole
		// helper lifetime. An artificial session-count cap would permanently
		// disable voice after enough successful tasks.
		p.seen[e.SessionID] = struct{}{}
		generation := p.epoch.Start()
		if generation == 0 {
			return result
		}
		p.current = &task{sessionID: e.SessionID, clientGeneration: e.Generation, epoch: generation}
		result.Allowed = true
		return result
	}

	t := p.current
	if t == nil || t.finished || t.clientGeneration != e.Generation || t.sessionID != e.SessionID {
		return result
	}
	result.Stage = t.stage
	if e.Type == EventTimeout && e.Stage == StageRecording && t.stage == StageRecording {
		if p.epoch.RecordingLimit(t.epoch) {
			t.stage = StageCaptureStop
			p.epoch.StartStage(t.epoch, StageCaptureStop, StageBudget(StageCaptureStop, 0, 0))
			result.Stage = StageCaptureStop
			result.Reason = ReasonRecordLimit
			result.Effects = []Effect{p.effect(t, "stop_native_capture", ReasonRecordLimit)}
			result.Allowed = true
		} else {
			result.Allowed = p.epoch.Valid(t.epoch)
		}
		return result
	}
	if !p.epoch.Valid(t.epoch) {
		// The bridge invokes the *same* exported Go cancellation function as
		// Windows before requesting the effects. Its context is revoked
		// immediately; the matching event still emits exactly one effects batch.
		if e.Type == EventCancel {
			reason := e.Reason
			if reason == "" {
				reason = ReasonUser
			}
			if p.epoch.Reason() == reason {
				result.Reason = reason
				result.Effects = p.finish(t, reason)
				result.Allowed = true
				return result
			}
		}
		if p.epoch.Reason() == ReasonTimeout {
			result.Reason = ReasonTimeout
			result.Effects = p.finish(t, ReasonTimeout)
		}
		return result
	}
	switch e.Type {
	case EventStage:
		budget := StageBudget(e.Stage, e.AudioBytes, e.RecordSeconds)
		if budget == 0 || !p.epoch.StartStage(t.epoch, e.Stage, budget) {
			return result
		}
		t.stage = e.Stage
		t.requestID = ""
		result.Stage = e.Stage
		result.BudgetMilliseconds = budget.Milliseconds()
		result.Allowed = true
	case EventBind:
		if e.RequestID == "" || t.stage == "" || (t.requestID != "" && t.requestID != e.RequestID) {
			return result
		}
		t.requestID = e.RequestID
		result.Allowed = true
	case EventAuthorize:
		if e.Stage != "" && t.stage != e.Stage {
			return result
		}
		result.Allowed = true
	case EventReceipt:
		if e.Stage != "" && t.stage != e.Stage {
			return result
		}
		if !p.epoch.FinishStage(t.epoch, t.stage) {
			return result
		}
		t.stage = ""
		t.requestID = ""
		result.Allowed = true
	case EventComplete:
		if t.stage != "" && !p.epoch.FinishStage(t.epoch, t.stage) {
			return result
		}
		t.finished = true
		p.epoch.Cancel(t.epoch, "")
		result.Effects = []Effect{p.effect(t, "clear_task", "")}
		result.Allowed = true
	case EventCancel:
		reason := e.Reason
		if reason == "" {
			reason = ReasonUser
		}
		result.Reason = reason
		result.Effects = p.finish(t, reason)
		result.Allowed = true
	case EventTimeout:
		if e.Stage == "" || t.stage != e.Stage {
			return result
		}
		// The native timer is only a wake-up. Go's monotonic stage clock owns
		// the decision; an early/stale timer cannot revoke the task.
		if p.epoch.Valid(t.epoch) {
			result.Allowed = true
			return result
		}
		if p.epoch.Reason() == ReasonTimeout {
			result.Reason = ReasonTimeout
			if t.stage == StageRecording {
				// Missing physical key-release stops capture and preserves PCM.
				// The host proceeds through its normal stop/ASR path.
				result.Effects = []Effect{p.effect(t, "stop_native_capture", ReasonRecordLimit)}
			} else {
				result.Effects = p.finish(t, ReasonTimeout)
			}
		}
	}
	return result
}
