package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"voicerewrite/internal/core/audio"
	coreconfig "voicerewrite/internal/core/config"
	meetingcore "voicerewrite/internal/core/meeting"
)

// meetingPacketSource adapts the macOS host's bounded PCM packets to the
// identical 20ms FrameSource used by the Windows Meeting pipeline.
type meetingPacketSource struct {
	packets chan []byte
}

func (s *meetingPacketSource) Stream(ctx context.Context, started func(), push func([]byte)) error {
	started()
	silence := make([]byte, audio.FrameBytes)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var lastAudio time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case packet := <-s.packets:
			for len(packet) >= audio.FrameBytes {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				push(packet[:audio.FrameBytes])
				packet = packet[audio.FrameBytes:]
			}
			lastAudio = time.Now()
		case <-tick.C:
			if !lastAudio.IsZero() && time.Since(lastAudio) > 300*time.Millisecond {
				push(silence)
			}
		}
	}
}

type bridgeMeeting struct {
	id        string
	runtime   *meetingcore.SessionRuntime
	pipeline  *meetingcore.Pipeline
	source    *meetingPacketSource
	audioDone chan struct{}
	config    coreconfig.Config
	scheduler *meetingcore.Scheduler

	mu                 sync.Mutex
	captions           []meetingcore.Caption
	prior              []string
	status             string
	level              int
	lastFinal          uint64
	dropped            int
	asrFirstMS         int64
	asrDoneMS          int64
	translationFirstMS int64
	translationDoneMS  int64
	replies            meetingcore.ReplyState
	view               meetingcore.HUDFlags
	replyStatus        string
	expression         meetingcore.Line
	expressionStatus   string
}

func newBridgeMeeting(cfg coreconfig.Config) (*bridgeMeeting, error) {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, err
	}
	m := &bridgeMeeting{
		id:        hex.EncodeToString(key[:]),
		runtime:   meetingcore.NewSessionRuntime(),
		source:    &meetingPacketSource{packets: make(chan []byte, 12)},
		audioDone: make(chan struct{}),
		status:    meetingcore.ListeningPrompt,
		config:    cfg,
		scheduler: &meetingcore.Scheduler{},
	}
	m.pipeline = meetingcore.NewPipeline(m.runtime, meetingcore.PipelineHooks{
		Transcribe: func(ctx context.Context, state *meetingcore.ASRState, turn meetingcore.AudioTurn, emit func(string)) (string, error) {
			var result string
			err := m.scheduler.Run(ctx, meetingcore.PriorityASR, func(active context.Context) error {
				var callErr error
				result, callErr = state.Transcribe(active, cfg, turn.PCM, emit)
				return callErr
			}, nil)
			return result, err
		},
		Translate: func(ctx context.Context, job meetingcore.CaptionTask, emit func(string)) error {
			return m.scheduler.Run(ctx, meetingcore.PriorityCaption, func(active context.Context) error {
				return meetingcore.Complete(active, cfg, meetingcore.Query{
					Mode: "caption", Latest: job.Latest, Prior: job.Prior,
				}, func(line meetingcore.Line) {
					if line.Kind == "ZH" {
						emit(line.Chinese)
					}
				})
			}, nil)
		},
		OnTranscript: func(turn meetingcore.AudioTurn, latest, stable string) []string {
			m.mu.Lock()
			defer m.mu.Unlock()
			if !m.runtime.Active(turn.Epoch) {
				return nil
			}
			m.captions = meetingcore.UpsertCaption(m.captions, turn.ID, latest, turn.Final, true)
			if !turn.Final && cfg.MeetingASRMode != coreconfig.MeetingASRMultipart {
				meetingcore.SetCaptionPrefix(m.captions, turn.ID, stable)
			}
			if len(m.captions) > 12 {
				m.captions = append([]meetingcore.Caption(nil), m.captions[len(m.captions)-12:]...)
			}
			prior := append([]string(nil), m.prior...)
			if turn.Final {
				m.prior = meetingcore.RememberPrior(m.prior, latest)
				m.lastFinal = turn.ID
				if strings.TrimSpace(latest) != "" {
					go func(id uint64) { _ = m.runReplies(m.runtime.Context(), id) }(turn.ID)
				}
			}
			m.status = meetingcore.ListeningPrompt
			return prior
		},
		OnTranslation: func(job meetingcore.CaptionTask, chinese string) {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.runtime.Active(job.Epoch) {
				meetingcore.ApplyCaptionTranslation(m.captions, job.ID, job.Latest, chinese)
			}
		},
		OnASRFirst: func(turn meetingcore.AudioTurn, elapsed time.Duration) {
			m.mu.Lock()
			if m.runtime.Active(turn.Epoch) {
				m.asrFirstMS = elapsed.Milliseconds()
			}
			m.mu.Unlock()
		},
		OnASRDone: func(turn meetingcore.AudioTurn, _ time.Duration, elapsed time.Duration, err error) {
			m.mu.Lock()
			if m.runtime.Active(turn.Epoch) {
				m.asrDoneMS = elapsed.Milliseconds()
				if err != nil {
					m.status = "会议识别失败，请检查 ASR 服务与模型"
				}
			}
			m.mu.Unlock()
		},
		OnTranslationFirst: func(job meetingcore.CaptionTask, elapsed time.Duration) {
			m.mu.Lock()
			if m.runtime.Active(job.Epoch) {
				m.translationFirstMS = elapsed.Milliseconds()
			}
			m.mu.Unlock()
		},
		OnTranslationDone: func(job meetingcore.CaptionTask, elapsed time.Duration, err error) {
			m.mu.Lock()
			if m.runtime.Active(job.Epoch) {
				m.translationDoneMS = elapsed.Milliseconds()
				if err != nil {
					m.status = meetingcore.TranslationUnavailable
				}
			}
			m.mu.Unlock()
		},
		OnDropped: func(epoch uint64) {
			m.mu.Lock()
			if m.runtime.Active(epoch) {
				m.dropped++
				m.status = meetingcore.DroppedWork
			}
			m.mu.Unlock()
		},
	})
	m.pipeline.Run()
	go m.runAudio()
	return m, nil
}

// runAudio restarts the shared segmenter for every active epoch. Pausing or
// resuming cannot accidentally revive buffered packets from an older turn.
func (m *bridgeMeeting) runAudio() {
	defer close(m.audioDone)
	for m.runtime.Context().Err() == nil {
		work, epoch, paused := m.runtime.Snapshot()
		if paused {
			select {
			case <-m.runtime.Context().Done():
				return
			case <-m.runtime.Wake():
				continue
			}
		}
		var turnID uint64
		_ = meetingcore.StreamAudio(work, m.source, func() time.Duration {
			m.mu.Lock()
			defer m.mu.Unlock()
			return time.Duration(m.asrDoneMS+m.translationDoneMS) * time.Millisecond
		}, func() {}, func(event meetingcore.AudioEvent) {
			if event.Meter {
				m.mu.Lock()
				if m.runtime.Active(epoch) {
					m.level = event.Level
				}
				m.mu.Unlock()
			}
			if event.UtteranceStarted {
				turnID, _ = m.runtime.NextTurn(epoch)
			}
			if event.PCM != nil && turnID != 0 {
				m.pipeline.QueueAudio(meetingcore.AudioTurn{
					ID: turnID, Epoch: epoch, Context: work,
					PCM: event.PCM, Final: event.Final, Ended: event.Time,
				})
			}
		})
	}
}

var (
	errMeetingBusy         = errors.New("meeting audio queue full")
	errMeetingPaused       = errors.New("meeting capture paused")
	errMeetingNoFinalTurn  = errors.New("no completed meeting turn")
	errMeetingInvalidReply = errors.New("selected reply is not complete")
)

func (m *bridgeMeeting) feed(pcm []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	work, _, paused := m.runtime.Snapshot()
	if m.runtime.Context().Err() != nil {
		return context.Canceled
	}
	if paused || work.Err() != nil {
		return errMeetingPaused
	}
	select {
	case m.source.packets <- pcm:
		return nil
	default:
		return errMeetingBusy
	}
}

func (m *bridgeMeeting) discardPacketsLocked() {
	for {
		select {
		case <-m.source.packets:
		default:
			return
		}
	}
}

// setPaused invalidates all in-flight work and buffered audio together.
// The native host must also stop ScreenCaptureKit while paused; the bridge
// independently refuses PCM even when the host has not yet stopped.
func (m *bridgeMeeting) setPaused(paused bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runtime.Context().Err() != nil {
		return context.Canceled
	}
	if m.runtime.SetPaused(paused) {
		m.discardPacketsLocked()
		m.level = 0
		m.replies.ResetWork()
		if paused {
			m.status = meetingcore.CapturePausedStatus
		} else {
			m.status = meetingcore.CaptureConnecting
		}
	}
	return nil
}

func (m *bridgeMeeting) stop() {
	m.mu.Lock()
	m.runtime.Stop()
	m.discardPacketsLocked()
	m.level = 0
	m.mu.Unlock()
	// Do not leave the capture consumer alive after a session is closed.
	<-m.audioDone
}

func (m *bridgeMeeting) runReplies(parent context.Context, expectedTurn uint64) error {
	m.mu.Lock()
	if expectedTurn != 0 && expectedTurn != m.lastFinal {
		m.mu.Unlock()
		return context.Canceled
	}
	var latest string
	for i := len(m.captions) - 1; i >= 0; i-- {
		if m.captions[i].Final && strings.TrimSpace(m.captions[i].Source) != "" {
			latest = m.captions[i].Source
			break
		}
	}
	_, epoch, paused := m.runtime.Snapshot()
	if status, ok := meetingcore.ReplyRequest(paused, latest != ""); !ok {
		if status != "" {
			m.replyStatus = status
		}
		m.mu.Unlock()
		if !paused {
			return errMeetingNoFinalTurn
		}
		return context.Canceled
	}
	turn := m.lastFinal
	// Automatic final-turn and manual requests must not race to infer the
	// same speech. A successful automatic batch is never inferred twice.
	if !m.replies.MayStartTurn(turn, expectedTurn != 0) {
		m.mu.Unlock()
		return nil
	}
	// Acquire cancellation ownership while holding the meeting lock: an older
	// goroutine cannot arrive late and cancel the newer turn's reply request.
	ctx, cancel, ok := m.runtime.ReplyContext(epoch, meetingcore.ReplyTimeout)
	if !ok {
		m.mu.Unlock()
		return context.Canceled
	}
	prior := meetingcore.ReplyPrior(m.prior, latest)
	serial := m.replies.StartTurn(turn)
	m.replyStatus = meetingcore.RepliesPreparing
	m.mu.Unlock()
	defer cancel()
	stopParent := context.AfterFunc(parent, cancel)
	defer stopParent()
	batch := meetingcore.ReplyBatch{Source: latest}
	err := m.scheduler.Run(ctx, meetingcore.PriorityReply, func(active context.Context) error {
		return meetingcore.Complete(active, m.config, meetingcore.Query{
			Mode: "replies", Latest: latest, Prior: prior,
		}, func(line meetingcore.Line) {
			if active.Err() != nil {
				return
			}
			m.mu.Lock()
			if m.runtime.Active(epoch) && m.replies.Current(serial) && batch.Apply(line) {
				m.replies.Publish(serial, batch)
			}
			m.mu.Unlock()
		})
	}, nil)
	if err == nil && !batch.HasReadyReply() {
		err = errors.New("meeting provider returned no completed suggestions")
	}
	m.mu.Lock()
	if m.replies.Current(serial) && m.runtime.Active(epoch) {
		m.replies.DiscardPartial()
		m.replies.FinishTurn(serial, err == nil && ctx.Err() == nil)
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			m.replyStatus = meetingcore.WorkCanceled
		case err != nil:
			m.replyStatus = meetingcore.RepliesFailed
		default:
			m.replyStatus = ""
		}
	}
	m.mu.Unlock()
	return err
}

// View actions use the same authoritative Go HUD transition function as
// Windows. The macOS host only decides how to lay out native SwiftUI panes.
func (m *bridgeMeeting) applyViewButton(id int) bool {
	if id != meetingcore.ButtonCaptions && id != meetingcore.ButtonExpression {
		return false
	}
	action, _ := meetingcore.CommandFor(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.view, _ = meetingcore.ApplyCommand(m.view, action, 0)
	return true
}

func (m *bridgeMeeting) acceptReplies() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.replies.AcceptPending()
}

func (m *bridgeMeeting) cancelWork() {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Keep cancellation and reply admission atomic: a new request may not
	// slip between canceling an old context and invalidating its generation.
	m.runtime.CancelReply()
	_, cancel, _, _ := m.runtime.NewExpression(time.Millisecond)
	cancel()
	m.replies.ResetWork()
	m.replyStatus = meetingcore.WorkCanceled
	m.expressionStatus = meetingcore.WorkCanceled
}

func (m *bridgeMeeting) refine(parent context.Context, index int, instruction string) error {
	if index < 0 || index >= 3 {
		return errors.New("invalid reply index")
	}
	m.mu.Lock()
	chosen := m.replies.Displayed.Lines[index]
	source := m.replies.Displayed.Source
	m.mu.Unlock()
	if !chosen.Complete || !meetingcore.RefineMayRun(chosen.English, false) ||
		!meetingcore.ValidInstruction(instruction) {
		return errMeetingInvalidReply
	}
	return m.runExpression(parent, meetingcore.Query{Mode: "refine",
		SelectedReply: chosen.English, UserPoints: instruction, Latest: source})
}

func (m *bridgeMeeting) compose(parent context.Context, instruction string) error {
	if !meetingcore.ValidInstruction(instruction) {
		return errors.New("invalid expression request")
	}
	return m.runExpression(parent, meetingcore.Query{Mode: "compose", UserPoints: instruction})
}

func (m *bridgeMeeting) runExpression(parent context.Context, query meetingcore.Query) error {
	ctx, cancel, epoch, serial := m.runtime.NewExpression(meetingcore.ExpressionTimeout)
	defer cancel()
	stopParent := context.AfterFunc(parent, cancel)
	defer stopParent()
	m.mu.Lock()
	if !m.runtime.CurrentExpression(epoch, serial) {
		m.mu.Unlock()
		return context.Canceled
	}
	m.expressionStatus = meetingcore.ExpressionLabel(query.Mode)
	m.mu.Unlock()
	var expression meetingcore.Line
	err := m.scheduler.Run(ctx, meetingcore.PriorityExpress, func(active context.Context) error {
		return meetingcore.Complete(active, m.config, query, func(line meetingcore.Line) {
			if line.Kind == "SAY" && line.Complete {
				expression = line
			}
		})
	}, nil)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runtime.CurrentExpression(epoch, serial) {
		if err == nil && expression.English != "" {
			m.expression = expression
			m.expressionStatus = ""
		} else if errors.Is(ctx.Err(), context.Canceled) {
			m.expressionStatus = meetingcore.WorkCanceled
		} else {
			m.expressionStatus = meetingcore.ExpressGenerateFailed
		}
	}
	return err
}

func (m *bridgeMeeting) snapshot() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	captions := make([]map[string]any, 0, len(m.captions))
	for _, c := range m.captions {
		captions = append(captions, map[string]any{
			"id": c.ID, "source": c.Source, "chinese": c.Chinese,
			"final": c.Final,
		})
	}
	lines := make([]map[string]any, 0, 3)
	for _, line := range m.replies.Displayed.Lines {
		lines = append(lines, map[string]any{"english": line.English, "chinese": line.Chinese, "complete": line.Complete})
	}
	_, epoch, paused := m.runtime.Snapshot()
	return map[string]any{
		"capture_paused": paused, "epoch": epoch,
		"metrics": map[string]any{
			"pending":      m.pipeline.Pending() + m.scheduler.Pending(),
			"dropped":      m.dropped,
			"asr_first_ms": m.asrFirstMS, "asr_done_ms": m.asrDoneMS,
			"translation_first_ms": m.translationFirstMS,
			"translation_done_ms":  m.translationDoneMS,
		},
		"replies": lines, "reply_source": m.replies.Displayed.Source,
		"reply_tip":         m.replies.Displayed.Tip,
		"reply_first_ready": meetingcore.FirstReadyReply(m.replies.Displayed.Lines),
		"reply_pending":     m.replies.PendingAvailable, "reply_status": m.replyStatus,
		"captions_only": m.view.CaptionsOnly, "expression_expanded": m.view.ExpressionExpanded,
		"expression": m.expression.English, "expression_chinese": m.expression.Chinese,
		"expression_status": m.expressionStatus,
		"session_id":        m.id, "status": m.status,
		"level": m.level, "captions": captions,
	}
}
