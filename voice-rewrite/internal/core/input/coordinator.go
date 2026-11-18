package input

import (
	"fmt"
	"sync"

	"voicerewrite/internal/core/voiceflow"
)

// Coordinator is the single Go input-to-task controller used by Windows and
// macOS. Flow owns physical gesture debouncing / device admission; the shared
// voiceflow.Engine owns task phases, exactly-once effects and receipt fencing.
// Hosts receive the same native effect actions and never run a Swift policy.
type Coordinator struct {
	mu      sync.Mutex
	edges   Flow
	engine  *voiceflow.Engine
	pending map[string]string
	latest  map[string]uint64
	serial  uint64
}

func (c *Coordinator) taskEvent(source string, generation uint64, typ voiceflow.EventType) voiceflow.Event {
	c.serial++
	return voiceflow.Event{
		Version:    voiceflow.Version,
		Type:       typ,
		SessionID:  "input/" + source,
		Generation: generation,
		CommandID:  fmt.Sprintf("edge/%d", c.serial),
	}
}

func (c *Coordinator) apply(e voiceflow.Event, out *Effect) voiceflow.Transition {
	t, err := c.engine.Apply(e)
	if err != nil {
		out.Decision = InputIgnore
		return voiceflow.Transition{}
	}
	out.Actions = append(out.Actions, t.Effects...)
	return t
}

func hasAction(actions []voiceflow.Action, typ voiceflow.ActionType) bool {
	for _, a := range actions {
		if a.Type == typ {
			return true
		}
	}
	return false
}

// Step preserves the typed stateful API for Go callers and deterministic tests.
func (c *Coordinator) Step(event Event) Effect {
	return InputEvent(c, event)
}

// InputEvent is the shared production entrypoint for both native hosts.
// This executes the complete state transition, not an adapter stub.
func InputEvent(c *Coordinator, event Event) Effect {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.engine == nil {
		c.engine = voiceflow.New()
		c.pending = make(map[string]string)
		c.latest = make(map[string]uint64)
	}
	out := c.edges.Step(event)
	if event.Kind == "route" || event.Kind == "suppress_next_side" || event.Kind == "allow_next_side" {
		return out
	}
	source := event.Source
	if event.Kind == "press" && out.Decision == InputStart {
		c.latest[source] = out.Generation
		mode := voiceflow.Dictation
		if event.Action == "rewrite" {
			mode = voiceflow.Rewrite
		}
		arm := c.taskEvent(source, out.Generation, voiceflow.RecordArm)
		arm.Mode = mode
		arm.CanInsert = event.TargetPresent
		if !c.apply(arm, &out).Accepted {
			out.Decision = InputIgnore
			c.edges.Step(Event{Kind: "cancel", Source: source})
			return out
		}
		press := c.taskEvent(source, out.Generation, voiceflow.Press)
		t := c.apply(press, &out)
		if !t.Accepted || !hasAction(t.Effects, voiceflow.StartNativeCapture) {
			out.Decision = InputIgnore
			c.edges.Step(Event{Kind: "cancel", Source: source})
			return out
		}
		for _, a := range t.Effects {
			if a.Type == voiceflow.StartNativeCapture {
				c.pending[source] = a.RequestID
			}
		}
		return out
	}
	generation := out.Generation
	if generation == 0 {
		generation = event.Generation
	}
	if event.Kind == "cancel" || event.Kind == "focus_changed" {
		// Flow consumes and advances its token for future native events.
		// voiceflow must cancel the PREVIOUS active task generation.
		generation = c.latest[source]
	}
	if generation == 0 || (event.Kind != "cancel" && event.Kind != "focus_changed" && generation != c.latest[source]) {
		return out
	}
	switch event.Kind {
	case "release":
		if out.Decision != InputStop && out.Decision != InputWait && out.Decision != InputClear {
			return out
		}
		t := c.apply(c.taskEvent(source, generation, voiceflow.Release), &out)
		if hasAction(t.Effects, voiceflow.StopNativeCapture) {
			out.Decision = InputStop
		} else if out.Decision == InputStop {
			out.Decision = InputIgnore
		}
	case "record_ready", "record_failed":
		if out.Decision == InputIgnore {
			return out
		}
		e := c.taskEvent(source, generation, voiceflow.RecordReady)
		e.Success = event.Kind == "record_ready"
		e.RequestID = c.pending[source]
		e.ReceiptID = c.pending[source]
		t := c.apply(e, &out)
		delete(c.pending, source)
		switch {
		case hasAction(t.Effects, voiceflow.StopNativeCapture):
			out.Decision = InputStop
		case t.Accepted && t.Phase == voiceflow.Recording:
			out.Decision = "record"
		case t.Phase == voiceflow.Failed:
			out.Decision = InputClear
		default:
			out.Decision = InputIgnore
		}
	case "timeout":
		e := c.taskEvent(source, generation, voiceflow.Timeout)
		e.TimeoutOrigin = "hold_watch"
		t := c.apply(e, &out)
		if hasAction(t.Effects, voiceflow.StopNativeCapture) {
			out.Decision = InputStop
		} else if out.Decision == InputStop {
			out.Decision = InputIgnore
		}
		delete(c.pending, source)
	case "cancel", "focus_changed":
		typ := voiceflow.Cancel
		if event.Kind == "focus_changed" {
			typ = voiceflow.FocusChanged
		}
		c.apply(c.taskEvent(source, generation, typ), &out)
		delete(c.pending, source)
	}
	return out
}
