// Package recording owns the portable lifetime, deadline and submission policy
// for non-Meeting voice capture. Hosts only perform native capture and effects.
package recording

import (
	"sync"
	"time"

	"voicerewrite/internal/core/config"
	"voicerewrite/internal/core/voiceflow"
)

// Reuse one private flow ID with increasing generations. The voiceflow Engine
// retains task state per session ID; a fresh UUID for every microphone hold
// would accumulate stale tasks in a long-running app. Native session IDs
// remain distinct and are validated by Controller before every event.
const recordingFlowSessionID = "__internal_non_meeting_recording__"

type Kind string

const (
	Dictation       Kind = "dictation"
	SpokenSelection Kind = "spoken_selection"
	ToggleSelection Kind = "toggle_selection"
)

type Cause string

const (
	Released Cause = "release"
	Deadline Cause = "deadline"
	Watchdog Cause = "watchdog"
)

// Action is a logical effect. It is emitted at most once for a generation,
// independent of repeated native stop, late key-up and timeout callbacks.
type Action string

const (
	None                Action = "none"
	SendASR             Action = "send_asr"
	SendSpokenSelection Action = "send_spoken_selection"
	RunToggleSelection  Action = "run_toggle_selection"
)

// Window is the only business source for the new default, stored duration,
// physical capture limit and the two-second lost-release safety margin.
// Native devices may impose stricter physical bounds for safety.
type Window struct {
	Seconds    int   `json:"seconds"`
	WatchdogMS int64 `json:"watchdog_ms"`
}

func Limits(requested int) Window {
	if requested < config.RecordSecondsMin || requested > config.RecordSecondsMax {
		requested = config.DefaultRecordSeconds
	}
	return Window{Seconds: requested, WatchdogMS: (time.Duration(requested)*time.Second + 2*time.Second).Milliseconds()}
}

func (w Window) Watchdog() time.Duration { return time.Duration(w.WatchdogMS) * time.Millisecond }

// KindForHold is shared by Windows and macOS. A toggle-selected recording
// is a different task from a held Mouse4 dictation or Mouse5 spoken rewrite.
func KindForHold(holdMode, dictation bool) Kind {
	if !holdMode {
		return ToggleSelection
	}
	if dictation {
		return Dictation
	}
	return SpokenSelection
}

// Route maps a stopped non-Meeting recording to exactly one portable worker.
// Meeting is deliberately absent: its independent lifecycle is not changed.
func Route(kind Kind) Action {
	switch kind {
	case Dictation:
		return SendASR
	case SpokenSelection:
		return SendSpokenSelection
	case ToggleSelection:
		return RunToggleSelection
	default:
		return None
	}
}

func ValidKind(kind Kind) bool { return Route(kind) != None }

// Decision is the effect receipt consumed by both native hosts.
// A refused/duplicate/stale event never authorizes sending audio.
type Decision struct {
	Receipt string `json:"receipt"`
	Action  Action `json:"action"`
	Window  Window `json:"window"`
}

type session struct {
	id           string
	generation   uint64
	kind         Kind
	window       Window
	committed    bool
	canceled     bool
	ready        bool
	startReceipt string
}

// Controller is session-scoped and safe for concurrent native timer/stop calls.
// A newer generation invalidates all old stop/timeout/completion callbacks.
type Controller struct {
	mu      sync.Mutex
	current session
	flow    *voiceflow.Engine
}

// NewController wires recording policy to the same Go voiceflow Engine
// used by voice_event, so all event receipts have one authoritative owner.
func NewController(engine *voiceflow.Engine) Controller {
	return Controller{flow: engine}
}

func (c *Controller) Begin(id string, generation uint64, kind Kind, requestedSeconds int) Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == "" || generation == 0 || !ValidKind(kind) {
		return Decision{Receipt: "invalid", Action: None}
	}
	if generation < c.current.generation {
		return Decision{Receipt: "stale", Action: None}
	}
	if generation == c.current.generation {
		if id == c.current.id && kind == c.current.kind && !c.current.canceled {
			return Decision{Receipt: "duplicate", Action: None, Window: c.current.window}
		}
		return Decision{Receipt: "stale", Action: None}
	}

	if c.flow == nil {
		c.flow = voiceflow.New()
	}
	mode := voiceflow.Dictation
	if kind == SpokenSelection || kind == ToggleSelection {
		mode = voiceflow.Rewrite
	}
	arm, err := c.flow.Apply(voiceflow.Event{
		Version: voiceflow.Version, Type: voiceflow.RecordArm,
		SessionID: recordingFlowSessionID, Generation: generation, CommandID: "record-arm",
		Mode: mode,
	})
	if err != nil || !arm.Accepted {
		return Decision{Receipt: "invalid", Action: None}
	}
	press, err := c.flow.Apply(voiceflow.Event{
		Version: voiceflow.Version, Type: voiceflow.Press,
		SessionID: recordingFlowSessionID, Generation: generation, CommandID: "press",
	})
	if err != nil || !press.Accepted {
		return Decision{Receipt: "invalid", Action: None}
	}
	var startReceipt string
	for _, effect := range press.Effects {
		if effect.Type == voiceflow.StartNativeCapture {
			startReceipt = effect.RequestID
		}
	}
	if startReceipt == "" {
		return Decision{Receipt: "invalid", Action: None}
	}
	c.current = session{id: id, generation: generation, kind: kind,
		window: Limits(requestedSeconds), startReceipt: startReceipt}
	return Decision{Receipt: "accepted", Action: None, Window: c.current.window}
}

// Ready acknowledges the actual native microphone start. The Go voiceflow
// reducer checks the matching start-effect receipt, not a host-owned flag.
func (c *Controller) Ready(id string, generation uint64, success bool) Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == "" || generation != c.current.generation || id != c.current.id || c.current.canceled {
		return Decision{Receipt: "stale", Action: None}
	}
	if c.current.ready {
		return Decision{Receipt: "duplicate", Action: None}
	}
	result, err := c.flow.Apply(voiceflow.Event{
		Version: voiceflow.Version, Type: voiceflow.RecordReady,
		SessionID: recordingFlowSessionID, Generation: generation, CommandID: "native-ready",
		RequestID: c.current.startReceipt, ReceiptID: c.current.startReceipt,
		Success: success,
	})
	if err != nil || !result.Accepted {
		return Decision{Receipt: "invalid", Action: None}
	}
	if !success {
		c.current.canceled = true
		return Decision{Receipt: "accepted", Action: None}
	}
	c.current.ready = true
	return Decision{Receipt: "accepted", Action: None, Window: c.current.window}
}

// Commit is called only after native PCM capture is stopped. Both a physical
// deadline and a release may race; only one can obtain a submission effect.
// A stale microphone completion from an old generation cannot send ASR.
func (c *Controller) Commit(id string, generation uint64, cause Cause) Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cause != Released && cause != Deadline && cause != Watchdog {
		return Decision{Receipt: "invalid", Action: None}
	}
	if id == "" || generation == 0 || generation != c.current.generation || id != c.current.id || c.current.canceled {
		return Decision{Receipt: "stale", Action: None}
	}

	if c.current.committed {
		return Decision{Receipt: "duplicate", Action: None, Window: c.current.window}
	}
	if !c.current.ready {
		return Decision{Receipt: "not_ready", Action: None}
	}
	eventType := voiceflow.Release
	if cause == Deadline || cause == Watchdog {
		eventType = voiceflow.RecordLimit
	}
	stopping, err := c.flow.Apply(voiceflow.Event{
		Version: voiceflow.Version, Type: eventType, SessionID: recordingFlowSessionID,
		Generation: generation, CommandID: "stop-" + string(cause),
	})
	if err != nil || !stopping.Accepted {
		return Decision{Receipt: "stale", Action: None}
	}
	var stopReceipt string
	for _, effect := range stopping.Effects {
		if effect.Type == voiceflow.StopNativeCapture {
			stopReceipt = effect.RequestID
		}
	}
	if stopReceipt == "" {
		return Decision{Receipt: "invalid", Action: None}
	}
	stopped, err := c.flow.Apply(voiceflow.Event{
		Version: voiceflow.Version, Type: voiceflow.AudioStopped,
		SessionID: recordingFlowSessionID, Generation: generation, CommandID: "audio-stopped",
		RequestID: stopReceipt, ReceiptID: stopReceipt, Success: true,
	})
	if err != nil || !stopped.Accepted {
		return Decision{Receipt: "invalid", Action: None}
	}
	for _, effect := range stopped.Effects {
		if effect.Type == voiceflow.SendASR {
			c.current.committed = true
			return Decision{Receipt: "accepted", Action: Route(c.current.kind), Window: c.current.window}
		}
	}
	return Decision{Receipt: "invalid", Action: None}
}

func (c *Controller) Cancel(id string, generation uint64) Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == "" || generation == 0 || generation != c.current.generation || id != c.current.id || c.current.canceled {
		return Decision{Receipt: "stale", Action: None}
	}

	_, err := c.flow.Apply(voiceflow.Event{
		Version: voiceflow.Version, Type: voiceflow.Cancel, SessionID: recordingFlowSessionID,
		Generation: generation, CommandID: "cancel",
	})
	if err != nil {
		return Decision{Receipt: "invalid", Action: None}
	}
	c.current.canceled = true
	return Decision{Receipt: "accepted", Action: None, Window: c.current.window}
}
