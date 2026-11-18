package input

import (
	"strings"
	"sync"

	"voicerewrite/internal/core/voiceflow"
)

// Event represents a host-normalized input boundary. Native hosts own the
// physical key identity, side-button timer, HID selection, microphone and AX.
type Event struct {
	Kind             string `json:"kind"`
	Source           string `json:"source,omitempty"`
	Action           string `json:"action,omitempty"`
	Generation       uint64 `json:"generation,omitempty"`
	PressID          string `json:"press_id,omitempty"`
	Paused           bool   `json:"paused,omitempty"`
	Busy             bool   `json:"busy,omitempty"`
	Starting         bool   `json:"starting,omitempty"`
	Recording        bool   `json:"recording,omitempty"`
	Processing       bool   `json:"processing,omitempty"`
	MeetingBusy      bool   `json:"meeting_busy,omitempty"`
	TargetPresent    bool   `json:"target_present,omitempty"`
	RequireTarget    bool   `json:"require_target,omitempty"`
	VoiceSelection   bool   `json:"voice_selection,omitempty"`
	TypedInstruction bool   `json:"typed_instruction,omitempty"`
	Instruction      string `json:"instruction,omitempty"`
	DeviceID         string `json:"device_id,omitempty"`
	BoundDeviceID    string `json:"bound_device_id,omitempty"`
}

// Effect is a portable decision. No effect performs an OS operation: hosts
// execute start/stop/cancel against the same generation and report ready.
type Effect struct {
	Decision   string             `json:"decision"`
	Generation uint64             `json:"generation,omitempty"`
	Actions    []voiceflow.Action `json:"effects,omitempty"`
}

const (
	InputIgnore             = "ignore"
	InputStart              = "start"
	InputStop               = "stop"
	InputWait               = "wait"
	InputClear              = "clear"
	InputMissingTarget      = "missing_target"
	InputMissingInstruction = "missing_instruction"
	InputTyped              = "typed"
	InputSpoken             = "spoken"
)

// InputAdmission is used by both native adapters. Dictation may be recorded
// without a text target (macOS copy fallback); a host can explicitly require
// one when its native workflow cannot proceed without it.
func InputAdmission(event Event) Effect {
	if event.Paused || event.Busy || event.Starting || event.Recording ||
		event.Processing || event.MeetingBusy ||
		(event.BoundDeviceID != "" && (event.DeviceID == "" || event.BoundDeviceID != event.DeviceID)) {
		return Effect{Decision: InputIgnore}
	}
	if event.RequireTarget && !event.TargetPresent {
		return Effect{Decision: InputMissingTarget}
	}
	return Effect{Decision: InputStart}
}

// InputRoute is the shared dictation / spoken-selection / typed-selection
// choice. An absent selection target never authorizes a native replacement.
func InputRoute(event Event) Effect {
	if event.Paused && !event.Recording {
		return Effect{Decision: InputIgnore}
	}
	if event.Action == "dictation" {
		if event.Recording {
			return Effect{Decision: InputStop}
		}
		return InputAdmission(event)
	}
	if event.Action != "rewrite" {
		return Effect{Decision: InputIgnore}
	}
	if event.Recording {
		if event.VoiceSelection && !event.TypedInstruction {
			return Effect{Decision: InputStop}
		}
		return Effect{Decision: InputIgnore}
	}
	if e := InputAdmission(event); e.Decision != InputStart {
		return e
	}
	if !event.TargetPresent {
		return Effect{Decision: InputMissingTarget}
	}
	if event.TypedInstruction {
		if strings.TrimSpace(event.Instruction) == "" {
			return Effect{Decision: InputMissingInstruction}
		}
		return Effect{Decision: InputTyped}
	}
	return Effect{Decision: InputSpoken}
}

type heldInput struct {
	source     string
	action     string
	device     string
	pressID    string
	generation uint64
	released   bool
	ready      bool
}

// Flow is the sole portable keyboard/side hold generation and release buffer.
// The value is safe for concurrent bridge requests. Do not cache this state
// in Swift; clients retain only the generation needed for OS effect receipts.
type Flow struct {
	mu             sync.Mutex
	next           uint64
	active         *heldInput
	ignoreNextSide bool
}

func (f *Flow) Step(event Event) Effect {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch event.Kind {
	case "route":
		return InputRoute(event)
	case "suppress_next_side":
		f.ignoreNextSide = true
		return Effect{Decision: InputClear}
	case "allow_next_side":
		f.ignoreNextSide = false
		return Effect{Decision: InputClear}
	case "press":
		if event.Source != "keyboard" && event.Source != "side" {
			return Effect{Decision: InputIgnore}
		}
		if event.Source == "side" && f.ignoreNextSide {
			f.ignoreNextSide = false
			return Effect{Decision: InputIgnore}
		}
		if f.active != nil {
			return Effect{Decision: InputIgnore}
		}
		if admission := InputAdmission(event); admission.Decision != InputStart {
			return admission
		}
		if event.Action != "dictation" && event.Action != "rewrite" {
			return Effect{Decision: InputIgnore}
		}
		f.next++
		f.active = &heldInput{source: event.Source, action: event.Action,
			device: event.DeviceID, pressID: event.PressID, generation: f.next}
		return Effect{Decision: InputStart, Generation: f.next}
	case "release", "record_ready", "record_failed", "timeout":
		active := f.active
		if active == nil || active.source != event.Source ||
			active.action != event.Action ||
			(event.Generation != 0 && active.generation != event.Generation) ||
			(event.PressID != "" && active.pressID != event.PressID) ||
			(event.Generation == 0 && (event.PressID == "" || active.pressID == "")) ||
			(active.device != "" && event.DeviceID != "" && active.device != event.DeviceID) {
			return Effect{Decision: InputIgnore}
		}
		token := active.generation
		switch event.Kind {
		case "release":
			if active.released {
				return Effect{Decision: InputIgnore, Generation: token}
			}
			active.released = true
			if event.Starting || !active.ready {
				// A Go-accepted press may still be waiting on a second
				// asynchronous selection-route IPC before native mic start.
				// Do not lose an early release in that gap.
				return Effect{Decision: InputWait, Generation: token}
			}
			f.active = nil
			if event.Recording {
				return Effect{Decision: InputStop, Generation: token}
			}
			return Effect{Decision: InputClear, Generation: token}
		case "record_ready":
			active.ready = true
			if active.released {
				f.active = nil
				return Effect{Decision: InputStop, Generation: token}
			}
			return Effect{Decision: "record", Generation: token}
		case "record_failed":
			f.active = nil
			return Effect{Decision: InputClear, Generation: token}
		case "timeout":
			f.active = nil
			if event.Recording || event.Starting {
				return Effect{Decision: InputStop, Generation: token}
			}
			return Effect{Decision: InputClear, Generation: token}
		}
	case "cancel", "focus_changed":
		if f.active != nil && event.Source != "" && f.active.source != event.Source {
			return Effect{Decision: InputIgnore}
		}
		f.next++
		f.active = nil
		return Effect{Decision: InputClear, Generation: f.next}
	}
	return Effect{Decision: InputIgnore}
}
