package meeting

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerRunPreemptsReplyWithoutOverlappingInference(t *testing.T) {
	var scheduler Scheduler
	var active atomic.Int32
	started := make(chan struct{})
	replyDone := make(chan error, 1)
	go func() {
		replyDone <- scheduler.Run(context.Background(), PriorityReply, func(ctx context.Context) error {
			if active.Add(1) != 1 {
				t.Error("multiple inference calls owned the GPU")
			}
			defer active.Add(-1)
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}, nil)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("reply did not start")
	}
	err := scheduler.Run(context.Background(), PriorityExpress, func(context.Context) error {
		if active.Load() != 0 {
			t.Error("express call overlapped canceled reply")
		}
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-replyDone:
		if !errors.Is(err, ErrPreempted) {
			t.Fatalf("reply should yield instead of retrying: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("preempted reply did not exit")
	}
}

func TestSchedulerRunRetriesInterruptedPreview(t *testing.T) {
	var scheduler Scheduler
	var attempts atomic.Int32
	first := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- scheduler.Run(context.Background(), PriorityPreview, func(ctx context.Context) error {
			if attempts.Add(1) == 1 {
				close(first)
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		}, nil)
	}()
	select {
	case <-first:
	case <-time.After(2 * time.Second):
		t.Fatal("preview did not start")
	}
	if err := scheduler.Run(context.Background(), PriorityExpress, func(context.Context) error { return nil }, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil || attempts.Load() != 2 {
			t.Fatalf("preview retry: attempts=%d error=%v", attempts.Load(), err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("preview was not retried")
	}
}

func TestSchedulerRunHonorsAlreadyCanceledContext(t *testing.T) {
	var scheduler Scheduler
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var called bool
	err := scheduler.Run(ctx, PriorityCaption, func(context.Context) error {
		called = true
		return nil
	}, nil)
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled work was admitted: error=%v called=%t", err, called)
	}
}
