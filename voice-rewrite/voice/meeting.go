package voice

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/config"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/audio"
	meetingcore "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/meeting"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/provider"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/speech"
)

type MeetingMetrics struct {
	Stage                                                        string
	Pending, Dropped                                             int
	ASRFirstMS, ASRDoneMS, TranslationFirstMS, TranslationDoneMS int64
}

type MeetingView struct {
	Status                             string
	Captions                           []meetingcore.Caption
	Reply                              meetingcore.ReplyState
	ExpressionStatus, ExpressionSource string
	Expression                         meetingcore.Line
	SourceName, ASRMode                string
	Level                              int
	CapturePaused                      bool
	Metrics                            MeetingMetrics
}

type MeetingSession struct {
	pipeline          *meetingcore.Pipeline
	runtime           *meetingcore.SessionRuntime
	cfg               coreconfig.Config
	logger            func(string, ...any)
	mu                sync.Mutex
	view              MeetingView
	prior             []string
	captureErr        error
	dirty, demo       bool
	modelMu           sync.Mutex
	model             string
	scheduler         meetingcore.Scheduler
	onConfig          func(coreconfig.Config)
	saveConfiguration func(coreconfig.Config) error
}

func NewMeetingSession(cfg coreconfig.Config, logger func(string, ...any), saveConfiguration func(coreconfig.Config) error) *MeetingSession {
	return &MeetingSession{runtime: meetingcore.NewSessionRuntime(), cfg: cfg, logger: logger, saveConfiguration: saveConfiguration}
}

func (s *MeetingSession) SetOnConfig(notify func(coreconfig.Config)) { s.onConfig = notify }
func (s *MeetingSession) SetDemo(demo bool) { s.demo = demo }

func (s *MeetingSession) Demo() bool { return s.demo }
func (s *MeetingSession) Context() context.Context { return s.runtime.Context() }
func (s *MeetingSession) Stop() { s.runtime.Stop() }
func (s *MeetingSession) Start(source audio.FrameSource) { s.start(source) }
func (s *MeetingSession) UpdateProfile(background, terms string) error { return s.updateProfile(background, terms) }
func (s *MeetingSession) SetASRMode(mode string) error { return s.setASRMode(mode) }
func (s *MeetingSession) RequestReplies() { s.requestReplies() }
func (s *MeetingSession) RefineReply(index int, instruction string) { s.refineReply(index, instruction) }
func (s *MeetingSession) AcceptPendingReplies() { s.acceptPendingReplies() }
func (s *MeetingSession) SetCapturePaused(paused bool) { s.setCapturePaused(paused) }

// Initialize fixes the shared Meeting session's initial presentation before
// the native host opens its window. The host supplies only its source label.
func (s *MeetingSession) Initialize(source string, demo *MeetingView) MeetingView {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view = MeetingView{Status: "正在连接会议音频…", SourceName: source, ASRMode: s.cfg.MeetingASRMode}
	if demo != nil {
		s.view = *demo
	}
	return s.view
}

// Refresh returns an immutable native presentation snapshot; the native host
// never mutates Meeting's task generations, reply queue or model state.
func (s *MeetingSession) Refresh() (MeetingView, error, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return MeetingView{}, nil, false
	}
	s.dirty = false
	view := s.view
	view.Captions = append([]meetingcore.Caption(nil), view.Captions...)
	return view, s.captureErr, true
}

// Express is the shared Meeting voice transaction. Hosts only capture native
// audio and display the resulting snapshot, error and progress.
func (s *MeetingSession) Express(parent context.Context, pcm []byte, captureErr error, mode string) error {
	if err := parent.Err(); err != nil {
		return err
	}
	if s.runtime.Context().Err() != nil {
		return context.Canceled
	}
	s.mu.Lock()
	ctx, cancel, epoch, serial := s.runtime.NewExpression(meetingcore.ExpressionTimeout)
	s.mu.Unlock()
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	defer cancel()
	label := meetingcore.ExpressionLabel(mode)
	s.applyExpression(epoch, serial, func() {
		if !meetingcore.KeepFinishedEnglish(s.view.Expression.Complete, s.view.Expression.English) {
			s.view.Expression = meetingcore.Line{}
		}
		s.view.ExpressionSource = ""
		s.view.ExpressionStatus = label
	})
	failed := func(label string, cause error) error {
		if parent.Err() != nil {
			return parent.Err()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.applyExpression(epoch, serial, func() {
			s.view.ExpressionStatus = label + "：" + cause.Error()
			if !meetingcore.KeepFinishedEnglish(s.view.Expression.Complete, s.view.Expression.English) {
				s.view.Expression = meetingcore.Line{}
			}
		})
		return fmt.Errorf("%s: %w", label, cause)
	}
	if label, detail, halted := meetingcore.ExpressCapture(captureErr, audio.BelowMinCapture(pcm)); halted {
		cause := captureErr
		if detail != "" {
			cause = errors.New(detail)
		}
		return failed(label, cause)
	}
	var points string
	cfg := s.configSnapshot()
	err := s.runInference(ctx, meetingcore.PriorityExpress, meetingcore.ExpressRecognizing, func(active context.Context) error {
		var e error
		points, e = speech.Transcribe(active, cfg, pcm)
		return e
	})
	if parent.Err() != nil {
		return parent.Err()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var detail string
	var halted bool
	label, detail, points, halted = meetingcore.ExpressTranscript(err, points)
	if halted {
		cause := err
		if detail != "" {
			cause = errors.New(detail)
		}
		return failed(label, cause)
	}
	label = meetingcore.ExpressionLabel(mode)
	s.applyExpression(epoch, serial, func() {
		s.view.ExpressionSource = points
		s.view.ExpressionStatus = label
	})
	var prior []string
	if mode == "compose" {
		s.mu.Lock()
		prior = append([]string(nil), s.prior...)
		s.mu.Unlock()
	}
	s.logger("meeting expression mode=%s recognized_chars=%d", mode, len([]rune(points)))
	err = s.complete(ctx, meetingcore.PriorityExpress, meetingcore.Query{Mode: mode, Prior: prior, UserPoints: points}, func(line meetingcore.Line) {
		if parent.Err() != nil || line.Kind != "SAY" {
			return
		}
		s.applyExpression(epoch, serial, func() {
			s.view.Expression = line
			s.view.ExpressionStatus = ""
		})
	})
	if parent.Err() != nil {
		return parent.Err()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return failed(meetingcore.ExpressGenerateFailed, err)
	}
	return nil
}

func (s *MeetingSession) mutate(update func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtime.Context().Err() != nil {
		return
	}
	update()
	s.dirty = true
}
func (s *MeetingSession) applyWork(epoch uint64, update func()) {
	s.mutate(func() {
		if s.runtime.Active(epoch) {
			update()
		}
	})
}
func (s *MeetingSession) configSnapshot() coreconfig.Config { s.mu.Lock(); defer s.mu.Unlock(); return s.cfg }

// resetViewLocked runs with the host state mutex held after the runtime
// invalidates its epoch. Presentation never survives a canceled partial task.
func (s *MeetingSession) resetViewLocked() {
	s.view.Reply.ResetWork()
	clear, status := meetingcore.CancelPresentation(s.view.Expression.Complete, s.view.ExpressionStatus)
	if clear {
		s.view.Expression = meetingcore.Line{}
	}
	s.view.ExpressionStatus = status
	s.view.Captions = meetingcore.KeepCompletedCaptions(s.view.Captions)
	s.view.Level = 0
	s.view.Metrics.Pending = 0
}

func (s *MeetingSession) resetWorkLocked() {
	s.runtime.ResetWork()
	s.resetViewLocked()
}
func (s *MeetingSession) setCapturePaused(paused bool) {
	s.mutate(func() {
		if !s.runtime.SetPaused(paused) {
			return
		}
		s.view.CapturePaused = paused
		s.resetViewLocked()
		if paused {
			s.view.Status = meetingcore.CapturePausedStatus
			s.view.Metrics.Stage = meetingcore.CapturePausedStage
		} else {
			s.view.Status = meetingcore.CaptureConnecting
			s.view.Metrics.Stage = meetingcore.ListeningStage
		}
	})
}
func (s *MeetingSession) saveMeetingConfig(change func(*coreconfig.Config)) error {
	cfg := s.configSnapshot()
	change(&cfg)
	if s.saveConfiguration == nil {
		return errors.New(meetingcore.MeetingSaveUnavailable)
	}
	if err := s.saveConfiguration(cfg); err != nil {
		return err
	}
	s.mutate(func() {
		s.cfg = cfg
		s.resetWorkLocked()
		s.view.ASRMode = cfg.MeetingASRMode
	})
	if s.onConfig != nil {
		s.onConfig(cfg)
	}
	return nil
}
func (s *MeetingSession) updateProfile(background, terms string) error {
	background, terms, err := meetingcore.AcceptProfile(background, terms)
	if err != nil {
		return err
	}
	return s.saveMeetingConfig(func(c *coreconfig.Config) {
		c.MeetingBackground = background
		c.MeetingTerms = terms
	})
}
func (s *MeetingSession) setASRMode(mode string) error {
	if !coreconfig.KnownMeetingASRMode(mode) {
		return errors.New(meetingcore.UnknownMeetingASRMode)
	}
	return s.saveMeetingConfig(func(c *coreconfig.Config) { c.MeetingASRMode = mode })
}
func (s *MeetingSession) runInference(ctx context.Context, priority int, stage string, call func(context.Context) error) error {
	return s.scheduler.Run(ctx, priority, call, func(running bool) {
		s.mutate(func() {
			if running {
				s.view.Metrics.Stage = stage
			} else if !s.view.CapturePaused {
				s.view.Metrics.Stage = meetingcore.ListeningStage
			}
			s.view.Metrics.Pending = s.scheduler.Pending()
		})
	})
}

func (s *MeetingSession) updateCaption(epoch, id uint64, source, chinese string) {
	s.applyWork(epoch, func() {
		meetingcore.ApplyCaptionTranslation(s.view.Captions, id, source, chinese)
	})
}
func (s *MeetingSession) complete(ctx context.Context, priority int, query meetingcore.Query, emit func(meetingcore.Line)) error {
	cfg := s.configSnapshot()
	stage := meetingcore.InferenceStage(query.Mode, priority == meetingcore.PriorityExpress)
	return s.runInference(ctx, priority, stage, func(active context.Context) error {
		s.modelMu.Lock()
		if s.model == "" {
			model, err := provider.DiscoverModel(active, cfg)
			if err != nil {
				s.modelMu.Unlock()
				return err
			}
			s.model = model
		}
		cfg.TextModel = s.model
		s.modelMu.Unlock()
		return meetingcore.Complete(active, cfg, query, func(line meetingcore.Line) {
			if active.Err() == nil {
				emit(line)
			}
		})
	})
}
func (s *MeetingSession) queueReplies(job meetingcore.CaptionTask) {
	s.mu.Lock()
	if !s.runtime.Active(job.Epoch) {
		s.mu.Unlock()
		return
	}
	ctx, cancel, ok := s.runtime.ReplyContext(job.Epoch, meetingcore.ReplyTimeout)
	if !ok {
		s.mu.Unlock()
		return
	}
	serial := s.view.Reply.Next()
	s.view.Status = meetingcore.RepliesPreparing
	s.dirty = true
	s.mu.Unlock()
	go func() {
		defer cancel()
		batch := meetingcore.ReplyBatch{Source: job.Latest}
		err := s.complete(ctx, meetingcore.PriorityReply, meetingcore.Query{Mode: "replies", Latest: job.Latest, Prior: job.Prior}, func(line meetingcore.Line) {
			if !batch.Apply(line) {
				return
			}
			// Epoch, generation, and context are checked under the session
			// lock before partial output reaches the HUD.
			s.applyWork(job.Epoch, func() {
				if ctx.Err() == nil {
					s.view.Reply.Publish(serial, batch)
				}
			})
		})
		if err != nil {
			s.applyWork(job.Epoch, func() {
				if s.view.Reply.Current(serial) {
					s.view.Reply.DiscardPartial()
					if ctx.Err() == nil {
						s.view.Status = meetingcore.RepliesFailed
					}
				}
			})
		}
	}()
}
func (s *MeetingSession) acceptPendingReplies() {
	s.mutate(func() { s.view.Reply.AcceptPending() })
}
func (s *MeetingSession) requestReplies() {
	s.mu.Lock()
	last, hasCaption := meetingcore.LatestCompletedCaption(s.view.Captions)
	if status, start := meetingcore.ReplyRequest(s.view.CapturePaused, hasCaption); !start {
		if status != "" {
			s.view.Status = status
			s.dirty = true
		}
		s.mu.Unlock()
		return
	}
	prior := meetingcore.ReplyPrior(s.prior, last.Source)
	work, epoch, _ := s.runtime.Snapshot()
	job := meetingcore.CaptionTask{Latest: last.Source, ID: last.ID, Epoch: epoch, Context: work, Prior: prior, Ended: time.Now()}
	s.mu.Unlock()
	s.queueReplies(job)
}
func (s *MeetingSession) refineReply(index int, instruction string) {
	instruction, ok := meetingcore.AcceptRefine(index, instruction)
	if !ok {
		return
	}
	s.mu.Lock()
	selected := s.view.Reply.Displayed.Lines[index].English
	if selected == "" || s.view.CapturePaused {
		s.mu.Unlock()
		return
	}
	ctx, cancel, epoch, serial := s.runtime.NewExpression(meetingcore.ReplyTimeout)
	s.view.ExpressionStatus = meetingcore.RefineRunning
	s.dirty = true
	s.mu.Unlock()
	go func() {
		defer cancel()
		err := s.complete(ctx, meetingcore.PriorityExpress, meetingcore.Query{Mode: "refine", SelectedReply: selected, UserPoints: instruction}, func(line meetingcore.Line) {
			s.applyExpression(epoch, serial, func() { s.view.Expression = line; s.view.ExpressionStatus = "" })
		})
		if err != nil && ctx.Err() == nil {
			s.applyExpression(epoch, serial, func() {
				s.view.ExpressionStatus = meetingcore.RefineFailed
				if !s.view.Expression.Complete {
					s.view.Expression = meetingcore.Line{}
				}
			})
		}
	}()
}

func (s *MeetingSession) applyExpression(epoch, serial uint64, update func()) {
	s.applyWork(epoch, func() {
		if s.runtime.CurrentExpression(epoch, serial) {
			update()
		}
	})
}

func (s *MeetingSession) start(source audio.FrameSource) {
	s.pipeline = meetingcore.NewPipeline(s.runtime, meetingcore.PipelineHooks{
		Transcribe: func(ctx context.Context, state *meetingcore.ASRState, turn meetingcore.AudioTurn, emit func(string)) (string, error) {
			cfg := s.configSnapshot()
			priority := meetingcore.PriorityPreview
			if turn.Final {
				priority = meetingcore.PriorityASR
			}
			var latest string
			err := s.runInference(ctx, priority, meetingcore.CaptionRecognizing, func(active context.Context) error {
				var e error
				latest, e = state.Transcribe(active, cfg, turn.PCM, emit)
				return e
			})
			return latest, err
		},
		Translate: func(ctx context.Context, job meetingcore.CaptionTask, emit func(string)) error {
			return s.complete(ctx, meetingcore.PriorityCaption, meetingcore.Query{Mode: "caption", Prior: job.Prior, Latest: job.Latest}, func(line meetingcore.Line) {
				if line.Kind == "ZH" {
					emit(line.Chinese)
				}
			})
		},
		OnTranscript: func(turn meetingcore.AudioTurn, latest, stable string) []string {
			var prior []string
			s.applyWork(turn.Epoch, func() {
				s.view.Captions = meetingcore.UpsertCaption(s.view.Captions, turn.ID, latest, turn.Final, true)
				if !turn.Final && s.cfg.MeetingASRMode != coreconfig.MeetingASRMultipart {
					meetingcore.SetCaptionPrefix(s.view.Captions, turn.ID, stable)
				}
				prior = append([]string(nil), s.prior...)
				if turn.Final {
					s.prior = meetingcore.RememberPrior(s.prior, latest)
				}
				s.view.Status = meetingcore.ListeningPrompt
			})
			return prior
		},
		OnTranslation: func(job meetingcore.CaptionTask, chinese string) {
			s.updateCaption(job.Epoch, job.ID, job.Latest, chinese)
		},
		OnASRFirst: func(turn meetingcore.AudioTurn, elapsed time.Duration) {
			s.applyWork(turn.Epoch, func() { s.view.Metrics.ASRFirstMS = elapsed.Milliseconds() })
		},
		OnASRDone: func(turn meetingcore.AudioTurn, first, elapsed time.Duration, err error) {
			s.applyWork(turn.Epoch, func() {
				s.view.Metrics.ASRDoneMS = elapsed.Milliseconds()
				if err != nil {
					s.view.Status = err.Error()
				}
			})
			if err != nil {
				s.logger("meeting stage=asr error=%v", err)
			} else {
				s.logger("meeting stage=asr first_ms=%d complete_ms=%d since_snapshot_ms=%d final=%t", first.Milliseconds(), elapsed.Milliseconds(), time.Since(turn.Ended).Milliseconds(), turn.Final)
			}
		},
		OnTranslationFirst: func(job meetingcore.CaptionTask, elapsed time.Duration) {
			s.applyWork(job.Epoch, func() { s.view.Metrics.TranslationFirstMS = elapsed.Milliseconds() })
			s.logger("meeting stage=caption first_ms=%d since_snapshot_ms=%d", elapsed.Milliseconds(), time.Since(job.Ended).Milliseconds())
		},
		OnTranslationDone: func(job meetingcore.CaptionTask, elapsed time.Duration, err error) {
			s.applyWork(job.Epoch, func() {
				s.view.Metrics.TranslationDoneMS = elapsed.Milliseconds()
				if err != nil {
					s.view.Status = meetingcore.TranslationUnavailable
				}
			})
			if err != nil {
				s.logger("meeting stage=caption error=%v", err)
			}
		},
		OnDropped: func(epoch uint64) {
			s.applyWork(epoch, func() {
				s.view.Metrics.Dropped++
				s.view.Status = meetingcore.DroppedWork
			})
		},
	})
	s.pipeline.Run()
	go func() {
		for s.runtime.Context().Err() == nil {
			s.mu.Lock()
			work, epoch, paused := s.runtime.Snapshot()
			s.mu.Unlock()
			if paused {
				select {
				case <-s.runtime.Context().Done():
					return
				case <-s.runtime.Wake():
					continue
				}
			}
			var id uint64
			err := meetingcore.StreamAudio(work, source, func() time.Duration {
				s.mu.Lock()
				defer s.mu.Unlock()
				return time.Duration(s.view.Metrics.ASRDoneMS+s.view.Metrics.TranslationDoneMS) * time.Millisecond
			}, func() {
				s.applyWork(epoch, func() { s.view.Status = meetingcore.ListeningPrompt })
			}, func(event meetingcore.AudioEvent) {
				if event.Meter {
					pending := s.pipeline.Pending() + s.scheduler.Pending()
					s.applyWork(epoch, func() { s.view.Level = event.Level; s.view.Metrics.Pending = pending })
				}
				if event.UtteranceStarted {
					s.applyWork(epoch, func() {
						id, _ = s.runtime.NextTurn(epoch)
						s.view.Reply.DiscardPartial()
						s.view.Reply.Next()
						s.runtime.CancelReply()
					})
				}
				if event.PCM != nil {
					s.pipeline.QueueAudio(meetingcore.AudioTurn{
						Final: event.Final, ID: id, Epoch: epoch, Context: work,
						PCM: event.PCM, Ended: event.Time,
					})
				}
			})
			if work.Err() != nil {
				continue
			}
			if err == nil {
				err = errors.New(meetingcore.CaptureEnded)
			}
			s.applyWork(epoch, func() { s.captureErr = err })
			return
		}
	}()
}
