// Package session owns the monotonically increasing generation and cancellation
// policy for an in-flight voice task. Native adapters still verify the actual
// target window, selection and clipboard immediately before writing.
package session

import (
	"context"
	"sync"
	"time"
)

type Epoch struct {
	mu            sync.Mutex
	currentID     uint64
	closed        bool
	active        bool
	ctx           context.Context
	cancel        context.CancelFunc
	stage         Stage
	stageStarted  time.Time
	stageDeadline time.Time
	reason        Reason
}

// Start fences all old callbacks and cancels the previous provider context.
// Once the counter is exhausted it never wraps into a previously used token.
func (g *Epoch) Start() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.currentID == ^uint64(0) {
		return 0
	}
	if g.cancel != nil {
		g.cancel()
	}
	g.currentID++
	g.active = true
	g.reason = ""
	g.stage, g.stageStarted, g.stageDeadline = "", time.Time{}, time.Time{}
	g.ctx, g.cancel = context.WithCancel(context.Background())
	return g.currentID
}

func (g *Epoch) validLocked(id uint64) bool {
	if id == 0 || g.closed || !g.active || g.currentID != id {
		return false
	}
	if !g.stageDeadline.IsZero() && !time.Now().Before(g.stageDeadline) {
		g.cancelLocked(ReasonTimeout)
		return false
	}
	return true
}

func (g *Epoch) Valid(id uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.validLocked(id)
}

// StartStage starts a *stage-local* monotonic deadline, not a deadline
// measured from microphone press. A stale task cannot reset a new task's clock.
func (g *Epoch) StartStage(id uint64, stage Stage, budget time.Duration) bool {
	if stage == "" || budget <= 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.validLocked(id) {
		return false
	}
	now := time.Now()
	g.stage, g.stageStarted, g.stageDeadline = stage, now, now.Add(budget)
	return true
}

// FinishStage clears a completed deadline so a late timer cannot cancel a
// successful write or a subsequent stage.
// Recording-limit expiry is not an error: keep the generation alive, clear
// only the recording clock and let the native host deliver the captured PCM.
func (g *Epoch) RecordingLimit(id uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if id == 0 || g.closed || !g.active || g.currentID != id ||
		g.stage != StageRecording || g.stageDeadline.IsZero() ||
		time.Now().Before(g.stageDeadline) {
		return false
	}
	g.stage, g.stageStarted, g.stageDeadline = "", time.Time{}, time.Time{}
	return true
}

func (g *Epoch) FinishStage(id uint64, stage Stage) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.validLocked(id) || g.stage != stage {
		return false
	}
	g.stage, g.stageStarted, g.stageDeadline = "", time.Time{}, time.Time{}
	return true
}

// Cancel is idempotent. It revokes only the exact current generation, without
// allocating a fresh task; Start can immediately create the next one.
func (g *Epoch) Cancel(id uint64, reason Reason) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.validLocked(id) {
		return false
	}
	g.cancelLocked(reason)
	return true
}

func (g *Epoch) cancelLocked(reason Reason) {
	g.active = false
	g.reason = reason
	g.stageDeadline = time.Time{}
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
}

func (g *Epoch) Reason() Reason {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reason
}

// ContextFor returns a canceled context for any stale or expired task.
func (g *Epoch) ContextFor(id uint64) context.Context {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.validLocked(id) && g.ctx != nil {
		return g.ctx
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func (g *Epoch) Current() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.currentID
}

func (g *Epoch) Shutdown() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	g.active = false
	if g.currentID < ^uint64(0) {
		g.currentID++
	}
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
	g.ctx = nil
	g.stageDeadline = time.Time{}
}
