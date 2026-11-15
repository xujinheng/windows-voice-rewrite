package meeting

import (
	"context"
	"errors"
	"sync"
)

const (
	PriorityExpress = iota
	PriorityASR
	PriorityCaption
	PriorityPreview
	PriorityReply
)

var ErrPreempted = errors.New("meeting inference yielded to higher priority work")

type meetingWaiter struct {
	priority int
	ctx      context.Context
	run      context.Context
	cancel   context.CancelCauseFunc
	ready    chan struct{}
}

// Cancellation requests preemption; ownership changes only on release, after
// the old HTTP call has returned. This prevents overlap on a shared GPU.
type Scheduler struct {
	mu      sync.Mutex
	active  *meetingWaiter
	waiters []*meetingWaiter
}

func (g *Scheduler) Acquire(ctx context.Context, priority int) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	run, cancel := context.WithCancelCause(ctx)
	w := &meetingWaiter{priority: priority, ctx: ctx, run: run, cancel: cancel, ready: make(chan struct{})}
	g.mu.Lock()
	g.waiters = append(g.waiters, w)
	if g.active != nil && (priority == PriorityExpress || g.active.priority == PriorityReply) && priority < g.active.priority {
		g.active.cancel(ErrPreempted)
	}
	g.advanceLocked()
	g.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			g.mu.Lock()
			cancel(context.Canceled)
			if g.active == w {
				g.active = nil
			}
			for i, q := range g.waiters {
				if q == w {
					g.waiters = append(g.waiters[:i], g.waiters[i+1:]...)
					break
				}
			}
			g.advanceLocked()
			g.mu.Unlock()
		})
	}
	select {
	case <-ctx.Done():
		release()
		return nil, nil, ctx.Err()
	case <-w.ready:
		if err := ctx.Err(); err != nil {
			release()
			return nil, nil, err
		}
		return run, release, nil
	}
}
// Run serializes inference with higher-priority work. An interrupted ASR or
// caption attempt retries after its replacement releases the GPU; background
// reply suggestions instead stop so they do not delay fresher speech.
func (g *Scheduler) Run(ctx context.Context, priority int, call func(context.Context) error, onPhase func(running bool)) error {
	for {
		active, release, err := g.Acquire(ctx, priority)
		if err != nil {
			return err
		}
		if onPhase != nil {
			onPhase(true)
		}
		err = call(active)
		preempted := errors.Is(context.Cause(active), ErrPreempted)
		release()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if preempted {
			if priority == PriorityReply {
				return ErrPreempted
			}
			continue
		}
		if onPhase != nil {
			onPhase(false)
		}
		return err
	}
}

func (g *Scheduler) advanceLocked() {
	if g.active != nil {
		return
	}
	for len(g.waiters) > 0 {
		best := 0
		for i := range g.waiters {
			if g.waiters[i].priority < g.waiters[best].priority {
				best = i
			}
		}
		w := g.waiters[best]
		g.waiters = append(g.waiters[:best], g.waiters[best+1:]...)
		if w.ctx.Err() != nil {
			w.cancel(w.ctx.Err())
			continue
		}
		g.active = w
		close(w.ready)
		return
	}
}
func (g *Scheduler) Pending() int { g.mu.Lock(); defer g.mu.Unlock(); return len(g.waiters) }
