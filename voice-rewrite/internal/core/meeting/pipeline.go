package meeting

import (
	"context"
	"errors"
	"sync"
	"time"
)

// AudioTurn is an immutable ASR snapshot from a portable audio stream.
type AudioTurn struct {
	ID, Epoch uint64
	Final     bool
	PCM       []byte
	Ended     time.Time
	Context   context.Context
}

// CaptionTask identifies the exact transcript revision a translation belongs to.
type CaptionTask struct {
	ID, Epoch, Version uint64
	Final              bool
	Prior              []string
	Latest             string
	Ended              time.Time
	Context            context.Context
}

// PipelineHooks isolate model inference and presentation from task orchestration.
// Hooks run synchronously and must not call Pipeline methods.
type PipelineHooks struct {
	Transcribe         func(context.Context, *ASRState, AudioTurn, func(string)) (string, error)
	Translate          func(context.Context, CaptionTask, func(string)) error
	OnTranscript       func(AudioTurn, string, string) []string
	OnTranslation      func(CaptionTask, string)
	OnASRFirst         func(AudioTurn, time.Duration)
	OnASRDone          func(AudioTurn, time.Duration, time.Duration, error)
	OnTranslationFirst func(CaptionTask, time.Duration)
	OnTranslationDone  func(CaptionTask, time.Duration, error)
	OnDropped          func(uint64)
}

// Pipeline owns ASR/translation queues, revision checks and preview preemption.
// SessionRuntime is the sole authority for pause, resume and shutdown epochs.
type Pipeline struct {
	runtime *SessionRuntime
	hooks   PipelineHooks

	finals   chan AudioTurn
	previews chan AudioTurn
	captions chan CaptionTask

	mu              sync.Mutex
	epoch           uint64
	latestFinal     uint64
	revision        uint64
	latestRevision  map[uint64]uint64
	activeCancel    context.CancelFunc
	activeID        uint64
	activeFinal     bool
}

func NewPipeline(runtime *SessionRuntime, hooks PipelineHooks) *Pipeline {
	return &Pipeline{
		runtime: runtime, hooks: hooks,
		finals: make(chan AudioTurn, 2), previews: make(chan AudioTurn, 1),
		captions: make(chan CaptionTask, 1),
		latestRevision: make(map[uint64]uint64),
	}
}

func (p *Pipeline) Run() {
	go p.transcribeTurns()
	go p.translateTurns()
}

func (p *Pipeline) Pending() int {
	return len(p.finals) + len(p.previews) + len(p.captions)
}

func (p *Pipeline) dropped(epoch uint64) {
	if p.hooks.OnDropped != nil {
		p.hooks.OnDropped(epoch)
	}
}

// QueueAudio never blocks capture. A final supersedes the same turn's preview,
// including an in-flight ASR request, and has its own priority queue.
func (p *Pipeline) QueueAudio(turn AudioTurn) {
	if turn.Context == nil || turn.Context.Err() != nil || turn.ID == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.runtime.Active(turn.Epoch) || (p.epoch != 0 && turn.Epoch < p.epoch) {
		return
	}
	if p.epoch != turn.Epoch {
		p.epoch = turn.Epoch
		p.latestFinal = 0
		p.latestRevision = make(map[uint64]uint64)
	}
	if turn.Final {
		if turn.ID < p.latestFinal {
			return
		}
		p.latestFinal = turn.ID
		p.runtime.CancelCaption(turn.ID)
		if p.activeCancel != nil && !p.activeFinal && p.activeID <= turn.ID {
			p.activeCancel()
		}
	} else if turn.ID <= p.latestFinal {
		return
	}
	queue := p.previews
	if turn.Final {
		queue = p.finals
	}
	select {
	case queue <- turn:
		return
	default:
	}
	select {
	case <-queue:
		p.dropped(turn.Epoch)
	default:
	}
	select {
	case queue <- turn:
	default:
		p.dropped(turn.Epoch)
	}
}

func (p *Pipeline) eligibleLocked(turn AudioTurn) bool {
	if turn.Context == nil || turn.Context.Err() != nil || p.epoch != turn.Epoch || !p.runtime.Active(turn.Epoch) {
		return false
	}
	if turn.Final {
		return turn.ID >= p.latestFinal
	}
	return turn.ID > p.latestFinal
}

func (p *Pipeline) transcribeTurns() {
	var state ASRState
	var currentID, currentEpoch uint64
	for {
		var turn AudioTurn
		// Drain a pending final before beginning provisional work.
		select {
		case <-p.runtime.Context().Done():
			return
		case turn = <-p.finals:
		default:
			select {
			case <-p.runtime.Context().Done():
				return
			case turn = <-p.finals:
			case turn = <-p.previews:
			}
		}
		p.mu.Lock()
		eligible := p.eligibleLocked(turn)
		p.mu.Unlock()
		if !eligible {
			continue
		}
		if currentID != turn.ID || currentEpoch != turn.Epoch {
			state = ASRState{}
			currentID, currentEpoch = turn.ID, turn.Epoch
		}
		p.transcribeTurn(turn, &state)
	}
}

func (p *Pipeline) transcribeTurn(turn AudioTurn, state *ASRState) {
	ctx, cancel := context.WithTimeout(turn.Context, 16*time.Second)
	defer cancel()
	p.mu.Lock()
	if !p.eligibleLocked(turn) {
		p.mu.Unlock()
		return
	}
	p.activeCancel, p.activeID, p.activeFinal = cancel, turn.ID, turn.Final
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.activeCancel = nil
		p.mu.Unlock()
	}()

	started := time.Now()
	var first time.Duration
	latest, err := p.hooks.Transcribe(ctx, state, turn, func(text string) {
		if ctx.Err() != nil {
			return
		}
		if first == 0 {
			first = time.Since(started)
			if p.hooks.OnASRFirst != nil {
				p.hooks.OnASRFirst(turn, first)
			}
		}
	})
	elapsed := time.Since(started)
	if turn.Context.Err() != nil || errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	p.mu.Lock()
	if !p.eligibleLocked(turn) || ctx.Err() == context.Canceled {
		p.mu.Unlock()
		return
	}
	var job CaptionTask
	if err == nil {
		p.revision++
		// Only a bounded number of recent utterances can receive translations.
		p.latestRevision[turn.ID] = p.revision
		if turn.ID > 64 {
			delete(p.latestRevision, turn.ID-64)
		}
		p.runtime.CancelCaption(turn.ID)
		var prior []string
		if p.hooks.OnTranscript != nil {
			prior = p.hooks.OnTranscript(turn, latest, state.StablePrefix())
		}
		job = CaptionTask{
			ID: turn.ID, Epoch: turn.Epoch, Version: p.revision, Final: turn.Final,
			Latest: latest, Prior: prior, Ended: turn.Ended, Context: turn.Context,
		}
	}
	p.mu.Unlock()
	if p.hooks.OnASRDone != nil {
		p.hooks.OnASRDone(turn, first, elapsed, err)
	}
	if err == nil {
		p.queueCaption(job)
	}
}

func (p *Pipeline) currentLocked(job CaptionTask) bool {
	return job.Context != nil && job.Context.Err() == nil &&
		p.epoch == job.Epoch && p.latestRevision[job.ID] == job.Version &&
		(job.Final || job.ID > p.latestFinal) &&
		p.runtime.Active(job.Epoch)
}

func (p *Pipeline) queueCaption(job CaptionTask) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.currentLocked(job) {
		return
	}
	select {
	case p.captions <- job:
		return
	default:
	}
	var old CaptionTask
	select {
	case old = <-p.captions:
	default:
	}
	// Pending final translations outrank new provisional translations.
	if old.Final && !job.Final && p.currentLocked(old) {
		job = old
	}
	p.dropped(job.Epoch)
	select {
	case p.captions <- job:
	default:
		p.dropped(job.Epoch)
	}
}

func (p *Pipeline) translateTurns() {
	for {
		select {
		case <-p.runtime.Context().Done():
			return
		case job := <-p.captions:
			p.translateTurn(job)
		}
	}
}

func (p *Pipeline) translateTurn(job CaptionTask) {
	p.mu.Lock()
	if !p.currentLocked(job) {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(job.Context, 16*time.Second)
	if !p.runtime.TrackCaption(job.Epoch, job.ID, cancel) {
		p.mu.Unlock()
		cancel()
		return
	}
	p.mu.Unlock()
	defer cancel()

	started := time.Now()
	displayed := false
	err := p.hooks.Translate(ctx, job, func(chinese string) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if ctx.Err() != nil || !p.currentLocked(job) || !TranslationFitsSource(job.Latest, chinese) {
			return
		}
		if p.hooks.OnTranslation != nil {
			p.hooks.OnTranslation(job, chinese)
		}
		if !displayed {
			displayed = true
			if p.hooks.OnTranslationFirst != nil {
				p.hooks.OnTranslationFirst(job, time.Since(started))
			}
		}
	})
	p.mu.Lock()
	valid := ctx.Err() == nil && p.currentLocked(job)
	if valid && p.hooks.OnTranslationDone != nil {
		p.hooks.OnTranslationDone(job, time.Since(started), err)
	}
	p.mu.Unlock()
}
