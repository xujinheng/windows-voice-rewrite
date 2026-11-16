// Package session owns the generation that cancels an in-flight voice task.
// A new generation cancels the previous context immediately. Hosts still
// re-check the target window, selection, and clipboard before writing.
package session

import (
	"context"
	"sync"
)

type Epoch struct {
	mu        sync.Mutex
	currentID uint64
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
}

func (g *Epoch) Start() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return 0
	}
	if g.cancel != nil {
		g.cancel()
	}
	g.currentID++
	g.ctx, g.cancel = context.WithCancel(context.Background())
	return g.currentID
}

func (g *Epoch) Valid(id uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return id != 0 && !g.closed && g.currentID == id
}

// ContextFor returns a canceled context for a stale generation so a delayed
// request ends without contacting the provider.
func (g *Epoch) ContextFor(id uint64) context.Context {
	g.mu.Lock()
	defer g.mu.Unlock()
	if id != 0 && !g.closed && g.currentID == id && g.ctx != nil {
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
	g.currentID++
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
	g.ctx = nil
}
