// Package voiceflow owns portable task transitions. A host delivers typed events,
// executes returned effects, and returns receipts. No OS device calls run here.
package voiceflow

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

const Version = 1

type EventType string

const (
	RecordArm    EventType = "record_arm"
	Press        EventType = "press"
	Release      EventType = "release"
	RecordReady  EventType = "record_ready"
	RecordLimit  EventType = "record_limit"
	AudioStopped EventType = "audio_stopped"
	ASRResult    EventType = "asr_result"
	ModelResult  EventType = "model_result"
	WriteReceipt EventType = "write_receipt"
	Cancel       EventType = "cancel"
	Timeout      EventType = "timeout"
	FocusChanged EventType = "focus_changed"
)

type ActionType string

const (
	StartNativeCapture ActionType = "start_native_capture"
	StopNativeCapture  ActionType = "stop_native_capture"
	SendASR            ActionType = "send_asr"
	SendModel          ActionType = "send_model"
	AttemptNativeWrite ActionType = "attempt_native_write"
	ShowPreview        ActionType = "show_preview"
	SetPhase           ActionType = "set_phase"
	AppendHistory      ActionType = "append_history"
	ClearTask          ActionType = "clear_task"
)

type Mode string

const (
	Dictation Mode = "dictation"
	Rewrite   Mode = "rewrite"
)

type Phase string

const (
	Armed      Phase = "armed"
	Starting   Phase = "starting"
	Recording  Phase = "recording"
	Stopping   Phase = "stopping"
	Recognizing Phase = "recognizing"
	Modeling   Phase = "modeling"
	Writing    Phase = "writing"
	Preview    Phase = "preview"
	Success    Phase = "success"
	Failed     Phase = "failed"
	Canceled   Phase = "canceled"
)

// CommandID identifies the incoming event, independently of the JSONL envelope
// request ID. ReceiptID and RequestID must identify the corresponding *outbound*
// effect. Generation is monotonic per session, including after cancellation.
// Success is the adapter's observed result, never an implied paste confirmation.
type Event struct {
	Version       int       `json:"v"`
	Type          EventType `json:"type"`
	SessionID     string    `json:"session_id"`
	Generation    uint64    `json:"generation"`
	CommandID     string    `json:"command_id"`
	RequestID     string    `json:"request_id,omitempty"`
	ReceiptID     string    `json:"receipt_id,omitempty"`
	TimeoutOrigin string    `json:"timeout_origin,omitempty"`
	Mode          Mode      `json:"mode,omitempty"`
	CanInsert     bool      `json:"can_insert,omitempty"`
	Text          string    `json:"text,omitempty"`
	Success       bool      `json:"success,omitempty"`
	Authorized    bool      `json:"authorized,omitempty"`
}

type Action struct {
	Type          ActionType `json:"type"`
	SessionID     string     `json:"session_id"`
	Generation    uint64     `json:"generation"`
	CommandID     string     `json:"command_id"`
	RequestID     string     `json:"request_id"`
	Phase         Phase      `json:"phase,omitempty"`
	Text          string     `json:"text,omitempty"`
	TimeoutOrigin string     `json:"timeout_origin,omitempty"`
}

type Transition struct {
	Accepted   bool     `json:"accepted"`
	Duplicate  bool     `json:"duplicate"`
	Reason     string   `json:"reason,omitempty"`
	SessionID  string   `json:"session_id"`
	Generation uint64   `json:"generation"`
	Phase      Phase    `json:"phase"`
	Effects    []Action `json:"effects"`
}

var (
	ErrVersion = errors.New("unsupported voiceflow event version")
	ErrEvent   = errors.New("invalid voiceflow event")
)

const maxCommandsPerGeneration = 4096

type task struct {
	generation uint64
	mode       Mode
	canInsert  bool
	phase      Phase
	released   bool
	pending    string
	text       string
	next       uint64
	seen       map[string]struct{}
}

type Engine struct {
	mu      sync.Mutex
	tasks   map[string]*task
	latest  map[string]uint64
}

// New constructs a process-local engine. Apply is thread-safe. It never
// invokes a native callback while holding its mutex; effects are immutable.
func New() *Engine {
	return &Engine{tasks: make(map[string]*task), latest: make(map[string]uint64)}
}

func (t *task) action(kind ActionType, ev Event) Action {
	t.next++
	command := fmt.Sprintf("%s/%d/%d", ev.SessionID, ev.Generation, t.next)
	return Action{
		Type: kind, SessionID: ev.SessionID, Generation: ev.Generation,
		CommandID: command, RequestID: command, Text: t.text,
		TimeoutOrigin: ev.TimeoutOrigin,
	}
}

func (t *task) emit(out *Transition, kind ActionType, ev Event) Action {
	a := t.action(kind, ev)
	out.Effects = append(out.Effects, a)
	return a
}

func (t *task) phaseTo(out *Transition, phase Phase, ev Event) {
	t.phase = phase
	a := t.emit(out, SetPhase, ev)
	a.Phase = phase
	out.Effects[len(out.Effects)-1] = a
	out.Phase = phase
}

func (t *task) pendingEffect(out *Transition, kind ActionType, phase Phase, ev Event) {
	t.phaseTo(out, phase, ev)
	a := t.emit(out, kind, ev)
	t.pending = a.CommandID
}

func (t *task) finish(out *Transition, phase Phase, ev Event) {
	t.pending = ""
	t.phaseTo(out, phase, ev)
	t.emit(out, ClearTask, ev)
}

func (t *task) abort(out *Transition, phase Phase, ev Event) {
	if t.phase == Starting || t.phase == Recording || t.phase == Stopping {
		t.emit(out, StopNativeCapture, ev)
	}
	t.finish(out, phase, ev)
}

// Apply consumes one event. A stale, out-of-order or repeated event is a
// no-effect transition; invalid event envelopes return an error. This prevents
// duplicate write actions even if the host resends receipts or provider replies.
func (e *Engine) Apply(ev Event) (Transition, error) {
	if ev.Version != Version {
		return Transition{}, ErrVersion
	}
	if len(ev.SessionID) == 0 || len(ev.SessionID) > 128 ||
		len(ev.CommandID) == 0 || len(ev.CommandID) > 128 ||
		ev.Generation == 0 {
		return Transition{}, ErrEvent
	}
	if ev.Type == Timeout && (ev.TimeoutOrigin == "" || len(ev.TimeoutOrigin) > 128) {
		return Transition{}, ErrEvent
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	out := Transition{SessionID: ev.SessionID, Generation: ev.Generation, Effects: []Action{}}
	t := e.tasks[ev.SessionID]
	if ev.Type == RecordArm {
		if ev.Mode != Dictation && ev.Mode != Rewrite {
			return Transition{}, ErrEvent
		}
		if ev.Generation <= e.latest[ev.SessionID] {
			out.Reason = "stale_generation"
			if t != nil { out.Phase = t.phase }
			return out, nil
		}
		if t != nil && !terminal(t.phase) {
			// Clear the old epoch before arming the new one. The old generation
			// cannot issue any further native write after this transition.
			old := Event{SessionID: ev.SessionID, Generation: t.generation}
			t.abort(&out, Canceled, old)
		}
		e.latest[ev.SessionID] = ev.Generation
		t = &task{generation: ev.Generation, mode: ev.Mode,
			canInsert: ev.CanInsert, phase: Armed,
			seen: map[string]struct{}{ev.CommandID: {}}}
		e.tasks[ev.SessionID] = t
		out.Accepted = true
		t.emit(&out, ShowPreview, ev)
		t.phaseTo(&out, Armed, ev)
		return out, nil
	}
	if t == nil || t.generation != ev.Generation {
		out.Reason = "stale_generation"
		if t != nil { out.Phase = t.phase }
		return out, nil
	}
	out.Phase = t.phase
	if _, seen := t.seen[ev.CommandID]; seen {
		out.Duplicate = true
		out.Reason = "duplicate_command"
		return out, nil
	}
	if len(t.seen) >= maxCommandsPerGeneration {
		return Transition{}, ErrEvent
	}
	if terminal(t.phase) {
		out.Reason = "terminal"
		return out, nil
	}
	needsReceipt := ev.Type == RecordReady || ev.Type == AudioStopped ||
		ev.Type == ASRResult || ev.Type == ModelResult || ev.Type == WriteReceipt
	if needsReceipt && (ev.ReceiptID == "" || ev.ReceiptID != t.pending || ev.RequestID != t.pending) {
		out.Reason = "stale_receipt"
		return out, nil
	}
	applied := false
	switch ev.Type {
	case Press:
		if t.phase != Armed { break }
		applied = true
		t.pendingEffect(&out, StartNativeCapture, Starting, ev)
	case Release, RecordLimit:
		if t.phase == Starting && !t.released {
			applied = true
			t.released = true
		} else if t.phase == Recording {
			applied = true
			t.pendingEffect(&out, StopNativeCapture, Stopping, ev)
		} else if t.phase == Armed && ev.Type == Release {
			applied = true
			t.finish(&out, Canceled, ev)
		} else { break }
	case RecordReady:
		if t.phase != Starting { break }
		applied = true
		if !ev.Success {
			t.finish(&out, Failed, ev)
		} else if t.released {
			t.pendingEffect(&out, StopNativeCapture, Stopping, ev)
		} else {
			t.pending = ""
			t.phaseTo(&out, Recording, ev)
		}
	case AudioStopped:
		if t.phase != Stopping { break }
		applied = true
		if !ev.Success {
			t.finish(&out, Failed, ev)
		} else {
			t.pendingEffect(&out, SendASR, Recognizing, ev)
		}
	case ASRResult:
		if t.phase != Recognizing { break }
		applied = true
		t.text = strings.TrimSpace(ev.Text)
		if !ev.Success || t.text == "" {
			t.finish(&out, Failed, ev)
		} else if t.mode == Rewrite {
			t.pendingEffect(&out, SendModel, Modeling, ev)
		} else {
			t.writeOrPreview(&out, ev, true)
		}
	case ModelResult:
		if t.phase != Modeling { break }
		applied = true
		t.text = strings.TrimSpace(ev.Text)
		if !ev.Success || t.text == "" {
			t.finish(&out, Failed, ev)
		} else {
			t.writeOrPreview(&out, ev, ev.Authorized)
		}
	case WriteReceipt:
		if t.phase != Writing { break }
		applied = true
		if ev.Success {
			t.emit(&out, AppendHistory, ev)
			t.finish(&out, Success, ev)
		} else {
			// A failed or uncertain receipt cannot authorize another paste.
			t.emit(&out, ShowPreview, ev)
			t.finish(&out, Preview, ev)
		}
	case Cancel, FocusChanged, Timeout:
		applied = true
		t.abort(&out, Canceled, ev)
	default:
		return Transition{}, ErrEvent
	}
	if !applied {
		out.Reason = "out_of_order"
		return out, nil
	}
	// Mark accepted no-effect Release while the recorder is starting, so it
	// cannot be lost while native capture is in flight.
	t.seen[ev.CommandID] = struct{}{}
	out.Accepted = true
	out.Phase = t.phase
	return out, nil
}

func (t *task) writeOrPreview(out *Transition, ev Event, authorized bool) {
	if authorized && t.canInsert {
		t.pendingEffect(out, AttemptNativeWrite, Writing, ev)
		return
	}
	t.emit(out, ShowPreview, ev)
	t.finish(out, Preview, ev)
}

func terminal(p Phase) bool {
	return p == Success || p == Failed || p == Preview || p == Canceled
}
