package voice

import (
	"context"
	"sync"
	"time"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/config"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/history"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/ux"
)

// Core is the only portable voice runtime. A client supplies physical device
// facts, audio and final OS effects; the core owns holds, task generations,
// cancellation, inference and output routing.
type Core struct {
	History   history.Store
	mu        sync.Mutex
	keys      map[int]*keyState
	owner     int
	roles     map[int]int
	nextToken uint64
	currentID uint64
	closed    bool
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

func (c *Core) key(code int) *keyState {
	if code <= 0 {
		return nil
	}
	return c.keys[code]
}

// Bind replaces the physical input mapping without changing an in-flight
// key's role. Keyboard and mouse can both trigger the same voice operation.
func (c *Core) Bind(dictation, rewrite []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	roles := make(map[int]int, len(dictation)+len(rewrite))
	for _, code := range dictation {
		if code > 0 {
			roles[code] = 1
		}
	}
	for _, code := range rewrite {
		if code > 0 {
			roles[code] = 2
		}
	}
	c.roles = roles
}

func (c *Core) role(code int) int { return c.roles[code] }

func (c *Core) Role(code int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.role(code)
}

// HeldRole is frozen at physical key-down, surviving hotkey reload.
func (c *Core) HeldRole(code int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if h := c.key(code); h != nil && h.down {
		return h.role
	}
	return 0
}

func (c *Core) Down(code int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.key(code)
	return h != nil && h.down
}
func (c *Core) Press(code int, busy bool) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.key(code)
	if c.closed || c.role(code) == 0 || h != nil && h.down || busy || c.owner != 0 {
		return 0
	}
	if h == nil {
		if c.keys == nil {
			c.keys = make(map[int]*keyState)
		}
		h = &keyState{}
		c.keys[code] = h
	}
	c.nextToken++
	h.token = c.nextToken
	h.role = c.role(code)
	h.down, h.active = true, false
	c.owner = code
	return h.token
}
func (c *Core) canActivate(code int, token uint64) bool {
	h := c.key(code)
	return h != nil && c.owner == code && h.down && !h.active && h.token == token
}
func (c *Core) CanActivate(code int, token uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.canActivate(code, token)
}
func (c *Core) Activate(code int, token uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || !c.canActivate(code, token) {
		return false
	}
	c.keys[code].active = true
	return true
}
func (c *Core) Release(code int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.key(code)
	if h == nil || !h.down {
		return ReleaseIgnored
	}
	primary := c.owner == code
	if primary {
		c.owner = 0
	}
	active := h.active
	h.down, h.active = false, false
	h.token++
	if active {
		return ReleaseActive
	}
	if primary {
		return ReleaseShort
	}
	return ReleaseOther
}
func (c *Core) Abandon() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.owner = 0
}
func (c *Core) Armed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.owner != 0
}
func (c *Core) Start() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.currentID++
	c.ctx, c.cancel = context.WithCancel(context.Background())
	return c.currentID
}
func (c *Core) Valid(id uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return id != 0 && !c.closed && c.currentID == id && c.ctx != nil
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
func (c *Core) Current() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentID
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
func (c *Core) Cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelLocked()
}
func (c *Core) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.cancelLocked()
}

// Execute is the platform-independent task entrypoint. No client can execute
// an expired task or circumvent the shared model deadlines and result routes.
func (c *Core) Execute(token uint64, cfg coreconfig.Config, pcm []byte, selected string, rewrite bool, onTranscribed func(string)) (Result, error) {
	result, err := run(c.ContextFor(token), cfg, pcm, selected, rewrite, onTranscribed)
	if err == nil && c.Valid(token) {
		title := ux.DictationHistoryTitle
		if rewrite {
			title = ux.HistoryTitle(result.Intent)
		}
		c.History.Remember(title, result.Text)
	}
	return result, err
}
