package meeting

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSessionRuntimeResetInvalidatesWorkAndTurnIDs(t *testing.T) {
	r := NewSessionRuntime()
	defer r.Stop()
	first, epoch, paused := r.Snapshot()
	if first.Err() != nil || paused || epoch != 1 || !r.Active(epoch) {
		t.Fatalf("unexpected initial session: epoch=%d paused=%t err=%v", epoch, paused, first.Err())
	}
	id, ok := r.NextTurn(epoch)
	if !ok || id == 0 {
		t.Fatal("initial utterance was rejected")
	}
	r.ResetWork()
	if first.Err() == nil || r.Active(epoch) {
		t.Fatal("old work was not invalidated")
	}
	next, later, paused := r.Snapshot()
	if paused || later != epoch+1 || next.Err() != nil {
		t.Fatalf("reset did not provide a new active epoch: %d paused=%t err=%v", later, paused, next.Err())
	}
	if _, ok := r.NextTurn(epoch); ok {
		t.Fatal("stale capture generated a new utterance")
	}
	if nextID, ok := r.NextTurn(later); !ok || nextID <= id {
		t.Fatal("reset recycled an ASR utterance ID")
	}
	select {
	case <-r.Wake():
	default:
		t.Fatal("reset did not notify capture loop")
	}
}

func TestSessionRuntimePauseResumeAndResetWhilePaused(t *testing.T) {
	r := NewSessionRuntime()
	defer r.Stop()
	start, initialEpoch, _ := r.Snapshot()
	if !r.SetPaused(true) || r.SetPaused(true) {
		t.Fatal("pause transition not idempotent")
	}
	if start.Err() == nil || r.Active(initialEpoch) {
		t.Fatal("pause did not cancel running tasks")
	}
	pausedWork, pausedEpoch, paused := r.Snapshot()
	if !paused || pausedWork.Err() == nil {
		t.Fatal("pause exposed a live capture context")
	}
	r.ResetWork()
	stillPaused, nextEpoch, paused := r.Snapshot()
	if !paused || stillPaused.Err() == nil || nextEpoch <= pausedEpoch {
		t.Fatal("configuration reset restarted capture while paused")
	}
	if _, ok := r.NextTurn(nextEpoch); ok {
		t.Fatal("paused session assigned a new utterance ID")
	}
	if !r.SetPaused(false) || r.SetPaused(false) {
		t.Fatal("resume transition not idempotent")
	}
	resumed, activeEpoch, paused := r.Snapshot()
	if paused || resumed.Err() != nil || !r.Active(activeEpoch) {
		t.Fatal("resume did not create new active capture work")
	}
	if r.Active(pausedEpoch) || r.Active(initialEpoch) {
		t.Fatal("obsolete work became valid after resuming")
	}
}

func TestSessionRuntimeExpressionGenerationAndCancellation(t *testing.T) {
	r := NewSessionRuntime()
	defer r.Stop()
	first, cancelFirst, epoch, oldSerial := r.NewExpression(time.Second)
	defer cancelFirst()
	if first.Err() != nil || !r.CurrentExpression(epoch, oldSerial) {
		t.Fatal("first expression was rejected")
	}
	next, cancelNext, secondEpoch, newSerial := r.NewExpression(time.Second)
	defer cancelNext()
	if first.Err() == nil || secondEpoch != epoch || oldSerial == newSerial {
		t.Fatal("new expression did not cancel the old request")
	}
	if !r.CurrentExpression(secondEpoch, newSerial) || r.CurrentExpression(epoch, oldSerial) {
		t.Fatal("expression generation did not reject stale output")
	}
	r.ResetWork()
	if next.Err() == nil || r.CurrentExpression(secondEpoch, newSerial) {
		t.Fatal("reset allowed old expression results")
	}
	_, stopFresh, freshEpoch, freshSerial := r.NewExpression(time.Second)
	defer stopFresh()
	if !r.CurrentExpression(freshEpoch, freshSerial) {
		t.Fatal("new expression after reset was rejected")
	}
	r.SetPaused(true)
	if r.CurrentExpression(freshEpoch, freshSerial) {
		t.Fatal("paused expression remained valid")
	}
}

func TestSessionRuntimeStopCancelsEverything(t *testing.T) {
	r := NewSessionRuntime()
	work, epoch, _ := r.Snapshot()
	expression, cancel, _, serial := r.NewExpression(time.Second)
	defer cancel()
	r.Stop()
	r.Stop()
	if r.Context().Err() == nil || work.Err() == nil || expression.Err() == nil {
		t.Fatal("session shutdown leaked a child context")
	}
	if r.Active(epoch) || r.CurrentExpression(epoch, serial) || r.SetPaused(true) {
		t.Fatal("closed session accepted work")
	}
	if id, ok := r.NextTurn(epoch); ok || id != 0 {
		t.Fatal("closed session admitted an utterance")
	}
}

func TestSessionRuntimeConcurrentEpochChecks(t *testing.T) {
	r := NewSessionRuntime()
	defer r.Stop()
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 70; i++ {
			r.SetPaused(i%2 == 0)
			r.ResetWork()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 70; i++ {
			_, epoch, _ := r.Snapshot()
			r.Active(epoch)
			r.NextTurn(epoch)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 70; i++ {
			ctx, cancel, epoch, serial := r.NewExpression(time.Second)
			_ = r.CurrentExpression(epoch, serial)
			cancel()
			select {
			case <-ctx.Done():
			default:
			}
		}
	}()
	wg.Wait()
}

func TestSessionRuntimeExpressionTimeout(t *testing.T) {
	r := NewSessionRuntime()
	defer r.Stop()
	ctx, cancel, _, _ := r.NewExpression(time.Nanosecond)
	defer cancel()
	select {
	case <-ctx.Done():
		if ctx.Err() != context.DeadlineExceeded {
			t.Fatal("unexpected cancellation cause:", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("expression deadline was not enforced")
	}
}
