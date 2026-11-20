package voice

import (
	"context"
	"sync"
	"time"

	coreconfig "voicerewrite/internal/core/config"
)

// Core is the only portable voice runtime. A client supplies physical device
// facts, audio and final OS effects; the core owns holds, task generations,
// cancellation, inference and output routing.
type Core struct {
	mu        sync.Mutex
	keys      [3]keyState
	owner     int
	nextToken uint64
	currentID uint64
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
}
type keyState struct {
	down, active bool
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
	if code <= 0 || code >= len(c.keys) {
		return nil
	}
	return &c.keys[code]
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
	if c.closed || h == nil || h.down || busy || c.owner != 0 {
		return 0
	}
	c.nextToken++
	h.token = c.nextToken
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
	for i := range c.keys {
		if c.keys[i].down {
			c.keys[i].down, c.keys[i].active = false, false
			c.keys[i].token++
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
	return run(c.ContextFor(token), cfg, pcm, selected, rewrite, onTranscribed)
}
