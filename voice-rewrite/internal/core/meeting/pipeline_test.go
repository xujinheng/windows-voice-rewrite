package meeting

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func receivePipeline[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("meeting pipeline did not complete")
		var zero T
		return zero
	}
}

func TestPipelineFinalCancelsActivePreview(t *testing.T) {
	r := NewSessionRuntime()
	defer r.Stop()
	work, epoch, _ := r.Snapshot()
	id, _ := r.NextTurn(epoch)
	started := make(chan struct{})
	published := make(chan string, 3)
	p := NewPipeline(r, PipelineHooks{
		Transcribe: func(ctx context.Context, _ *ASRState, turn AudioTurn, _ func(string)) (string, error) {
			if !turn.Final {
				close(started)
				<-ctx.Done()
				// Simulate a provider that returns a late provisional response.
				return "obsolete preview", nil
			}
			return "confirmed final", nil
		},
		Translate: func(_ context.Context, _ CaptionTask, _ func(string)) error { return nil },
		OnTranscript: func(_ AudioTurn, text, _ string) []string {
			published <- text
			return nil
		},
	})
	p.Run()
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: id})
	receivePipeline(t, started)
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: id, Final: true})
	if got := receivePipeline(t, published); got != "confirmed final" {
		t.Fatalf("stale preview published: %q", got)
	}
	select {
	case extra := <-published:
		t.Fatalf("unexpected additional transcript %q", extra)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestPipelineTranslationRejectsSupersededRevision(t *testing.T) {
	r := NewSessionRuntime()
	defer r.Stop()
	work, epoch, _ := r.Snapshot()
	id, _ := r.NextTurn(epoch)
	var sequence atomic.Int32
	firstStarted := make(chan struct{})
	translated := make(chan string, 3)
	p := NewPipeline(r, PipelineHooks{
		Transcribe: func(_ context.Context, _ *ASRState, _ AudioTurn, _ func(string)) (string, error) {
			if sequence.Add(1) == 1 {
				return "first revision", nil
			}
			return "second revision", nil
		},
		Translate: func(ctx context.Context, job CaptionTask, emit func(string)) error {
			if job.Latest == "first revision" {
				close(firstStarted)
				<-ctx.Done()
				emit("旧译文")
				return ctx.Err()
			}
			emit("第二版")
			return nil
		},
		OnTranslation: func(_ CaptionTask, chinese string) {
			translated <- chinese
		},
	})
	p.Run()
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: id})
	receivePipeline(t, firstStarted)
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: id})
	if got := receivePipeline(t, translated); got != "第二版" {
		t.Fatalf("superseded translation published: %q", got)
	}
	select {
	case stale := <-translated:
		t.Fatalf("obsolete translation leaked: %q", stale)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestPipelinePauseResumeCancelsEpoch(t *testing.T) {
	r := NewSessionRuntime()
	defer r.Stop()
	work, epoch, _ := r.Snapshot()
	id, _ := r.NextTurn(epoch)
	started := make(chan struct{})
	published := make(chan string, 2)
	p := NewPipeline(r, PipelineHooks{
		Transcribe: func(ctx context.Context, _ *ASRState, turn AudioTurn, _ func(string)) (string, error) {
			if turn.Epoch == epoch {
				close(started)
				<-ctx.Done()
				return "stale", nil
			}
			return "resumed", nil
		},
		Translate: func(_ context.Context, _ CaptionTask, _ func(string)) error { return nil },
		OnTranscript: func(_ AudioTurn, text, _ string) []string {
			published <- text
			return nil
		},
	})
	p.Run()
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: id, Final: true})
	receivePipeline(t, started)
	if !r.SetPaused(true) {
		t.Fatal("pause failed")
	}
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: id + 1, Final: true})
	if !r.SetPaused(false) {
		t.Fatal("resume failed")
	}
	next, resumedEpoch, _ := r.Snapshot()
	nextID, ok := r.NextTurn(resumedEpoch)
	if !ok {
		t.Fatal("resumed turn rejected")
	}
	p.QueueAudio(AudioTurn{Context: next, Epoch: resumedEpoch, ID: nextID, Final: true})
	if got := receivePipeline(t, published); got != "resumed" {
		t.Fatalf("old epoch leaked %q", got)
	}
}

func TestPipelineContinuousTurnsResetASRState(t *testing.T) {
	r := NewSessionRuntime()
	defer r.Stop()
	work, epoch, _ := r.Snapshot()
	first, _ := r.NextTurn(epoch)
	second, _ := r.NextTurn(epoch)
	values := make(chan string, 3)
	var calls int
	p := NewPipeline(r, PipelineHooks{
		Transcribe: func(_ context.Context, state *ASRState, turn AudioTurn, _ func(string)) (string, error) {
			calls++
			switch calls {
			case 1:
				if state.raw != "" {
					t.Error("unexpected initial ASR state")
				}
				state.raw = "first prefix"
			case 2:
				if state.raw != "first prefix" {
					t.Error("same turn lost continuation state")
				}
			case 3:
				if state.raw != "" {
					t.Error("new utterance inherited ASR prefix")
				}
			}
			return "recognized", nil
		},
		Translate: func(_ context.Context, _ CaptionTask, _ func(string)) error { return nil },
		OnTranscript: func(turn AudioTurn, _ string, _ string) []string {
			values <- "updated"
			return nil
		},
	})
	p.Run()
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: first})
	receivePipeline(t, values)
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: first, Final: true})
	receivePipeline(t, values)
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: second, Final: true})
	receivePipeline(t, values)
	if calls != 3 {
		t.Fatalf("ASR calls = %d", calls)
	}
}

func TestPipelineFinalPriorityUnderCongestion(t *testing.T) {
	r := NewSessionRuntime()
	defer r.Stop()
	work, epoch, _ := r.Snapshot()
	blocked, _ := r.NextTurn(epoch)
	started := make(chan struct{})
	release := make(chan struct{})
	published := make(chan AudioTurn, 8)
	var drops atomic.Int32
	p := NewPipeline(r, PipelineHooks{
		Transcribe: func(_ context.Context, _ *ASRState, turn AudioTurn, _ func(string)) (string, error) {
			if turn.ID == blocked {
				close(started)
				<-release
			}
			return "recognized", nil
		},
		Translate: func(_ context.Context, _ CaptionTask, _ func(string)) error { return nil },
		OnTranscript: func(turn AudioTurn, _ string, _ string) []string {
			published <- turn
			return nil
		},
		OnDropped: func(uint64) { drops.Add(1) },
	})
	p.Run()
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: blocked})
	receivePipeline(t, started)
	var newest uint64
	for i := 0; i < 6; i++ {
		newest, _ = r.NextTurn(epoch)
		p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: newest, Final: true})
	}
	following, _ := r.NextTurn(epoch)
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: following})
	close(release)
	if got := receivePipeline(t, published); got.ID != newest || !got.Final {
		t.Fatalf("final lost priority: %+v, want %d", got, newest)
	}
	if got := receivePipeline(t, published); got.ID != following || got.Final {
		t.Fatalf("new speech did not resume after final: %+v", got)
	}
	if drops.Load() == 0 {
		t.Fatal("queue overflow did not report discarded snapshots")
	}
}

func TestPipelineShutdownRejectsLateTranslation(t *testing.T) {
	r := NewSessionRuntime()
	work, epoch, _ := r.Snapshot()
	id, _ := r.NextTurn(epoch)
	started := make(chan struct{})
	var emitted atomic.Int32
	p := NewPipeline(r, PipelineHooks{
		Transcribe: func(_ context.Context, _ *ASRState, _ AudioTurn, _ func(string)) (string, error) {
			return "english", nil
		},
		Translate: func(ctx context.Context, _ CaptionTask, emit func(string)) error {
			close(started)
			<-ctx.Done()
			emit("已取消")
			return ctx.Err()
		},
		OnTranslation: func(_ CaptionTask, _ string) { emitted.Add(1) },
	})
	p.Run()
	p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: id, Final: true})
	receivePipeline(t, started)
	r.Stop()
	time.Sleep(30 * time.Millisecond)
	if emitted.Load() != 0 {
		t.Fatal("translation published after shutdown")
	}
}

func TestPipelineConcurrentSubmissionAndCancellation(t *testing.T) {
	r := NewSessionRuntime()
	defer r.Stop()
	var wg sync.WaitGroup
	p := NewPipeline(r, PipelineHooks{
		Transcribe: func(ctx context.Context, _ *ASRState, _ AudioTurn, _ func(string)) (string, error) {
			return "speech", ctx.Err()
		},
		Translate: func(ctx context.Context, _ CaptionTask, _ func(string)) error { return ctx.Err() },
	})
	p.Run()
	for k := 0; k < 3; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 70; i++ {
				work, epoch, paused := r.Snapshot()
				if !paused {
					if id, ok := r.NextTurn(epoch); ok {
						p.QueueAudio(AudioTurn{Context: work, Epoch: epoch, ID: id, Final: i%2 == 0})
					}
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			r.SetPaused(true)
			r.SetPaused(false)
		}
	}()
	wg.Wait()
}
