package meeting

import (
	"context"
	"sync"
	"time"
)

// SessionRuntime owns Meeting work epochs, pause/resume, source turn IDs and
// voice expression cancellation. Native hosts supply audio, storage and UI.
type SessionRuntime struct {
	mu               sync.Mutex
	ctx              context.Context
	stop             context.CancelFunc
	work             context.Context
	cancelWork       context.CancelFunc
	wake             chan struct{}
	epoch            uint64
	turnID           uint64
	paused           bool
	expressionSerial uint64
	stopExpression   context.CancelFunc
	stopReply        context.CancelFunc
	stopCaption      context.CancelFunc
	captionID        uint64
}

func NewSessionRuntime() *SessionRuntime {
	ctx, stop := context.WithCancel(context.Background())
	work, cancelWork := context.WithCancel(ctx)
	return &SessionRuntime{
		ctx: ctx, stop: stop, work: work, cancelWork: cancelWork,
		wake: make(chan struct{}, 1), epoch: 1,
	}
}

// Context and Wake remain stable over the runtime's lifetime.
func (s *SessionRuntime) Context() context.Context { return s.ctx }
func (s *SessionRuntime) Wake() <-chan struct{} { return s.wake }

func (s *SessionRuntime) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop()
	s.cancelWork()
	if s.stopExpression != nil {
		s.stopExpression()
		s.stopExpression = nil
	}
	if s.stopReply != nil {
		s.stopReply()
		s.stopReply = nil
	}
	if s.stopCaption != nil {
		s.stopCaption()
		s.stopCaption = nil
	}
}

func (s *SessionRuntime) Snapshot() (context.Context, uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.work, s.epoch, s.paused
}

func (s *SessionRuntime) activeLocked(epoch uint64) bool {
	return s.ctx.Err() == nil && !s.paused && s.epoch == epoch && s.work.Err() == nil
}

// Active rejects work that was superseded by a pause, resume or config reset.
func (s *SessionRuntime) Active(epoch uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeLocked(epoch)
}

func (s *SessionRuntime) resetLocked() {
	s.cancelWork()
	if s.stopExpression != nil {
		s.stopExpression()
		s.stopExpression = nil
	}
	if s.stopReply != nil {
		s.stopReply()
		s.stopReply = nil
	}
	if s.stopCaption != nil {
		s.stopCaption()
		s.stopCaption = nil
	}
	s.epoch++
	s.turnID++ // IDs cannot collide with canceled ASR snapshots.
	s.work, s.cancelWork = context.WithCancel(s.ctx)
	if s.paused {
		s.cancelWork()
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// ResetWork invalidates every outstanding work epoch, including expression
// requests, while preserving the user's current pause setting.
func (s *SessionRuntime) ResetWork() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetLocked()
}

// SetPaused cancels in-flight work and invalidates its epoch exactly once
// per state change. Resuming provides a fresh active work context.
func (s *SessionRuntime) SetPaused(paused bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil || s.paused == paused {
		return false
	}
	s.paused = paused
	s.resetLocked()
	return true
}

func (s *SessionRuntime) NextTurn(epoch uint64) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.activeLocked(epoch) {
		return 0, false
	}
	s.turnID++
	return s.turnID, true
}

// NewExpression cancels the previous expression and returns its unique serial
// together with the active epoch; a stale callback cannot replace its result.
func (s *SessionRuntime) NewExpression(timeout time.Duration) (context.Context, context.CancelFunc, uint64, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopExpression != nil {
		s.stopExpression()
	}
	s.expressionSerial++
	ctx, cancel := context.WithTimeout(s.work, timeout)
	s.stopExpression = cancel
	return ctx, cancel, s.epoch, s.expressionSerial
}

func (s *SessionRuntime) CurrentExpression(epoch, serial uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeLocked(epoch) && serial != 0 && s.expressionSerial == serial
}


// ReplyContext cancels any earlier reply task and binds a new one to the
// active epoch. A paused or superseded epoch cannot start a reply request.
func (s *SessionRuntime) ReplyContext(epoch uint64, timeout time.Duration) (context.Context, context.CancelFunc, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.activeLocked(epoch) {
		return nil, nil, false
	}
	if s.stopReply != nil {
		s.stopReply()
	}
	ctx, cancel := context.WithTimeout(s.work, timeout)
	s.stopReply = cancel
	return ctx, cancel, true
}

func (s *SessionRuntime) CancelReply() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopReply != nil {
		s.stopReply()
		s.stopReply = nil
	}
}

// TrackCaption makes the active translator cancelable by a newer ASR revision,
// pause/resume transition, or session shutdown.
func (s *SessionRuntime) TrackCaption(epoch, id uint64, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.activeLocked(epoch) {
		cancel()
		return false
	}
	if s.stopCaption != nil {
		s.stopCaption()
	}
	s.stopCaption = cancel
	s.captionID = id
	return true
}

func (s *SessionRuntime) CancelCaption(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopCaption != nil && s.captionID == id {
		s.stopCaption()
		s.stopCaption = nil
	}
}
