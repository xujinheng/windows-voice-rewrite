package voice

import (
	"context"
	"sync"
	"time"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/history"
	"github.com/xujinheng/windows-voice-rewrite/core/ux"
)

// Core is the only portable voice runtime. A client supplies physical device
// facts, audio and final OS effects; the core owns holds, task generations,
// cancellation, inference and output routing.
type Core struct {
	History   history.Store
	paused    bool
	mu        sync.Mutex
	keys      map[int]*keyState
	owner     int
	roles     map[int]int
	nextToken uint64
	currentID uint64
	closed    bool
	phase     EventKind
	ctx       context.Context
	cancel    context.CancelFunc
}
type keyState struct {
	down, active bool
	role         int
	token        uint64
}

const (
	NoTargetWindow       = "没有可用的目标窗口"
	ToggleUINotifyFailed = "无法通知 UI 线程准备录音"
	SideHoldThreshold    = 250 * time.Millisecond
)

func HoldModeLabel(dictation, meeting bool) string {
	switch {
	case meeting && dictation:
		return "会议直译"
	case meeting:
		return "会议意图"
	case dictation:
		return "听写"
	default:
		return "改写"
	}
}

const (
	ReleaseIgnored = iota
	ReleaseOther
	ReleaseActive
	ReleaseShort
)

// CommandKind is the single portable lifecycle vocabulary for any native or CLI client.
// Physical key numbers are adapter facts; Go decides ownership and cancellation.
type CommandKind uint8

const (
	CommandBind CommandKind = iota + 1
	CommandPress
	CommandActivate
	CommandRelease
	CommandAbandon
	CommandStart
	CommandCancel
	CommandShutdown
	CommandPause
	CommandResume
)

type Command struct {
	Kind      CommandKind
	Code      int
	Token     uint64
	Busy      bool
	Dictation []int
	Rewrite   []int
}

type EventKind uint8

const (
	EventRejected EventKind = iota
	EventBound
	EventPressed
	EventActivated
	EventReleased
	EventAbandoned
	EventStarted
	EventCanceled
	EventClosed
	EventProcessing
	EventTranscribed
	EventFinished
	EventFailed
	EventPaused
	EventResumed
)

// Event is the command receipt and asynchronous task progress vocabulary.
// Release contains the shared short-click/active-hold disposition.
type Event struct {
	Kind    EventKind `json:"kind"`
	Token   uint64    `json:"token,omitempty"`
	Role    int       `json:"role,omitempty"`
	Release int       `json:"release,omitempty"`
	Text    string    `json:"text,omitempty"`
	Result  Result    `json:"result,omitempty"`
	Error   string    `json:"error,omitempty"`
}

// State is an immutable snapshot of one physical input and one task generation.
// Zero key/token ask for global state; snapshots never mutate Core.
type State struct {
	Role        int       `json:"role"`
	HeldRole    int       `json:"heldRole"`
	Down        bool      `json:"down"`
	Armed       bool      `json:"armed"`
	CanActivate bool      `json:"canActivate"`
	Valid       bool      `json:"valid"`
	Current     uint64    `json:"current"`
	Phase       EventKind `json:"phase"`
	Closed      bool      `json:"closed"`
	Paused      bool      `json:"paused"`
}

func (c *Core) key(code int) *keyState {
	if code <= 0 {
		return nil
	}
	return c.keys[code]
}

func (c *Core) canActivate(code int, token uint64) bool {
	h := c.key(code)
	return h != nil && c.owner == code && h.down && !h.active && h.token == token
}

// Snapshot replaces fragmented Role/HeldRole/Down/Armed/Valid getters with
// one atomic view of portable state at a specific input and generation.
func (c *Core) Snapshot(code int, token uint64) State {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := State{
		Role: c.roles[code], Armed: c.owner != 0,
		Valid:       token != 0 && !c.closed && c.currentID == token && c.ctx != nil,
		CanActivate: !c.closed && c.canActivate(code, token),
		Current:     c.currentID, Phase: c.phase, Closed: c.closed, Paused: c.paused,
	}
	if h := c.key(code); h != nil {
		s.Down = h.down
		if h.down {
			s.HeldRole = h.role
		}
	}
	return s
}

// Apply is the only portable input/task-lifecycle command entrypoint.
// There is no second policy engine or compatibility forwarding facade.
func (c *Core) Apply(cmd Command) Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Event{Kind: EventRejected}
	}
	switch cmd.Kind {
	case CommandBind:
		roles := make(map[int]int, len(cmd.Dictation)+len(cmd.Rewrite))
		for _, code := range cmd.Dictation {
			if code > 0 {
				roles[code] = 1
			}
		}
		for _, code := range cmd.Rewrite {
			if code > 0 {
				roles[code] = 2
			}
		}
		c.roles = roles
		return Event{Kind: EventBound}
	case CommandPause:
		if c.owner != 0 || c.phase == EventStarted || c.phase == EventProcessing || c.phase == EventTranscribed {
			break
		}
		c.paused = true
		return Event{Kind: EventPaused}
	case CommandResume:
		c.paused = false
		return Event{Kind: EventResumed}
	case CommandPress:
		h := c.key(cmd.Code)
		role := c.roles[cmd.Code]
		if cmd.Code <= 0 || role == 0 || h != nil && h.down || cmd.Busy || c.owner != 0 || c.paused {
			break
		}
		if h == nil {
			if c.keys == nil {
				c.keys = make(map[int]*keyState)
			}
			h = &keyState{}
			c.keys[cmd.Code] = h
		}
		c.nextToken++
		h.token, h.role = c.nextToken, role
		h.down, h.active = true, false
		c.owner = cmd.Code
		return Event{Kind: EventPressed, Token: h.token, Role: role}
	case CommandActivate:
		if !c.canActivate(cmd.Code, cmd.Token) {
			break
		}
		c.keys[cmd.Code].active = true
		return Event{Kind: EventActivated, Token: cmd.Token, Role: c.keys[cmd.Code].role}
	case CommandRelease:
		h := c.key(cmd.Code)
		if h == nil || !h.down {
			return Event{Kind: EventReleased, Release: ReleaseIgnored}
		}
		primary := c.owner == cmd.Code
		if primary {
			c.owner = 0
		}
		active, role := h.active, h.role
		h.down, h.active = false, false
		h.token++
		action := ReleaseOther
		if active {
			action = ReleaseActive
		} else if primary {
			action = ReleaseShort
		}
		return Event{Kind: EventReleased, Role: role, Release: action}
	case CommandAbandon:
		c.owner = 0 // Retain physical key-up for native click replay.
		return Event{Kind: EventAbandoned}
	case CommandStart:
		if c.paused {
			break
		}
		if c.cancel != nil {
			c.cancel()
		}
		c.currentID++
		c.ctx, c.cancel = context.WithCancel(context.Background())
		c.phase = EventStarted
		return Event{Kind: EventStarted, Token: c.currentID}
	case CommandCancel:
		c.cancelLocked()
		c.phase = EventCanceled
		return Event{Kind: EventCanceled, Token: c.currentID}
	case CommandShutdown:
		c.closed = true
		c.cancelLocked()
		c.phase = EventClosed
		return Event{Kind: EventClosed, Token: c.currentID}
	}
	return Event{Kind: EventRejected}
}

// ContextFor is used only to bind distinct long-running native work (Meeting)
// to the current voice generation. Normal clients call Execute instead.
func (c *Core) ContextFor(id uint64) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id != 0 && !c.closed && c.currentID == id && c.ctx != nil {
		return c.ctx
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// Cancel stops a task and drops any held key ownership, including late timers.
func (c *Core) cancelLocked() {
	c.currentID++
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.ctx = nil
	c.owner = 0
	for _, key := range c.keys {
		if key.down {
			key.down, key.active = false, false
			key.token++
		}
	}
}

// Execute is the platform-independent task entrypoint. No client can execute
// an expired task or circumvent the shared model deadlines and result routes.
func (c *Core) Execute(token uint64, cfg coreconfig.Config, pcm []byte, selected string, rewrite bool, emit func(Event)) (Result, error) {
	c.mu.Lock()
	if token != 0 && c.currentID == token && c.ctx != nil && !c.closed {
		c.phase = EventProcessing
	}
	c.mu.Unlock()
	result, err := run(c.ContextFor(token), cfg, pcm, selected, rewrite, func(text string) {
		c.mu.Lock()
		valid := token != 0 && c.currentID == token && !c.closed && c.ctx != nil
		if valid {
			c.phase = EventTranscribed
		}
		c.mu.Unlock()
		if valid && emit != nil {
			emit(Event{Kind: EventTranscribed, Token: token, Text: text})
		}
	})
	c.mu.Lock()
	valid := token != 0 && c.currentID == token && !c.closed && c.ctx != nil
	if valid {
		if err == nil {
			c.phase = EventFinished
		} else {
			c.phase = EventFailed
		}
	}
	c.mu.Unlock()
	if !valid {
		return result, err
	}
	if err == nil {
		title := ux.DictationHistoryTitle
		result.Title = ux.DictationHistoryTitle
		if rewrite {
			title = ux.HistoryTitle(result.Intent)
			result.Title = ux.ResultCardTitle(result.Intent)
		}
		c.History.Remember(title, result.Text)
		if emit != nil {
			emit(Event{Kind: EventFinished, Token: token, Result: result})
		}
	} else if emit != nil {
		emit(Event{Kind: EventFailed, Token: token, Error: err.Error()})
	}
	return result, err
}
