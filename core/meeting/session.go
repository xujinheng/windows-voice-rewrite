package meeting

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/xujinheng/windows-voice-rewrite/core/audio"
	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/internal/speech"
	"github.com/xujinheng/windows-voice-rewrite/core/provider"
)

type Metrics struct {
	Stage                                                        string
	Pending, Dropped                                             int
	ASRFirstMS, ASRDoneMS, TranslationFirstMS, TranslationDoneMS int64
}

type View struct {
	Status                             string
	Captions                           []Caption
	Reply                              ReplyState
	ExpressionStatus, ExpressionSource string
	Expression                         Line
	SourceName, ASRMode                string
	Level                              int
	CapturePaused                      bool
	Metrics                            Metrics
}

// AudioSource is implemented by native clients, not by the Go engine.
// Push consecutive 20ms, 16kHz mono PCM16 frames; return on cancellation.
// This public signature allows independent Go modules to supply audio without
// importing a private package or reimplementing Meeting business logic.
type AudioSource interface {
	Stream(ctx context.Context, started func(), push func([]byte)) error
}

type Session struct {
	pipeline          *Pipeline
	runtime           *SessionRuntime
	cfg               coreconfig.Config
	logger            func(string, ...any)
	mu                sync.Mutex
	view              View
	prior             []string
	captureErr        error
	dirty, demo       bool
	modelMu           sync.Mutex
	model             string
	scheduler         Scheduler
	onConfig          func(coreconfig.Config)
	saveConfiguration func(coreconfig.Config) error
}

func NewSession(cfg coreconfig.Config, logger func(string, ...any), saveConfiguration func(coreconfig.Config) error, onConfig func(coreconfig.Config), demo bool) *Session {
	return &Session{runtime: NewSessionRuntime(), cfg: cfg, logger: logger, saveConfiguration: saveConfiguration, onConfig: onConfig, demo: demo}
}

func (s *Session) Demo() bool { return s.demo }

func (s *Session) Context() context.Context { return s.runtime.Context() }

func (s *Session) Stop() { s.runtime.Stop() }

// Initialize fixes the shared Meeting session's initial presentation before
// the native host opens its window. The host supplies only its source label.
func (s *Session) Initialize(source string, demo *View) View {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view = View{Status: "正在连接会议音频…", SourceName: source, ASRMode: s.cfg.MeetingASRMode}
	if demo != nil {
		s.view = *demo
	}
	view := s.view
	_, _, view.CapturePaused = s.runtime.Snapshot()
	return view
}

// Refresh returns an immutable native presentation snapshot; the native host
// never mutates Meeting's task generations, reply queue or model state.
func (s *Session) Refresh() (View, error, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return View{}, nil, false
	}
	s.dirty = false
	view := s.view
	_, _, view.CapturePaused = s.runtime.Snapshot()
	view.Captions = append([]Caption(nil), view.Captions...)
	return view, s.captureErr, true
}

// Express is the shared Meeting voice transaction. Hosts only capture native
// audio and display the resulting snapshot, error and progress.
func (s *Session) Express(parent context.Context, pcm []byte, captureErr error, mode string) error {
	if err := parent.Err(); err != nil {
		return err
	}
	if s.runtime.Context().Err() != nil {
		return context.Canceled
	}
	s.mu.Lock()
	ctx, cancel, epoch, serial := s.runtime.NewExpression(ExpressionTimeout)
	s.mu.Unlock()
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	defer cancel()
	label := ExpressionLabel(mode)
	s.applyExpression(epoch, serial, func() {
		if !KeepFinishedEnglish(s.view.Expression.Complete, s.view.Expression.English) {
			s.view.Expression = Line{}
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
			if !KeepFinishedEnglish(s.view.Expression.Complete, s.view.Expression.English) {
				s.view.Expression = Line{}
			}
		})
		return fmt.Errorf("%s: %w", label, cause)
	}
	if label, detail, halted := ExpressCapture(captureErr, audio.BelowMinCapture(pcm)); halted {
		cause := captureErr
		if detail != "" {
			cause = errors.New(detail)
		}
		return failed(label, cause)
	}
	var points string
	cfg := s.Config()
	err := s.runInference(ctx, PriorityExpress, ExpressRecognizing, func(active context.Context) error {
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
	label, detail, points, halted = ExpressTranscript(err, points)
	if halted {
		cause := err
		if detail != "" {
			cause = errors.New(detail)
		}
		return failed(label, cause)
	}
	label = ExpressionLabel(mode)
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
	err = s.complete(ctx, PriorityExpress, Query{Mode: mode, Prior: prior, UserPoints: points}, func(line Line) {
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
		return failed(ExpressGenerateFailed, err)
	}
	return nil
}

func (s *Session) mutate(update func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtime.Context().Err() != nil {
		return
	}
	update()
	s.dirty = true
}
func (s *Session) applyWork(epoch uint64, update func()) {
	s.mutate(func() {
		if s.runtime.Active(epoch) {
			update()
		}
	})
}
func (s *Session) Config() coreconfig.Config { s.mu.Lock(); defer s.mu.Unlock(); return s.cfg }

// resetViewLocked runs with the host state mutex held after the runtime
// invalidates its epoch. Presentation never survives a canceled partial task.
func (s *Session) resetViewLocked() {
	s.view.Reply.ResetWork()
	clear, status := CancelPresentation(s.view.Expression.Complete, s.view.ExpressionStatus)
	if clear {
		s.view.Expression = Line{}
	}
	s.view.ExpressionStatus = status
	s.view.Captions = KeepCompletedCaptions(s.view.Captions)
	s.view.Level = 0
	s.view.Metrics.Pending = 0
}

func (s *Session) resetWorkLocked() {
	s.runtime.ResetWork()
	s.resetViewLocked()
}
func (s *Session) SetCapturePaused(paused bool) {
	s.mutate(func() {
		if !s.runtime.SetPaused(paused) {
			return
		}
		s.resetViewLocked()
		if paused {
			s.view.Status = CapturePausedStatus
			s.view.Metrics.Stage = CapturePausedStage
		} else {
			s.view.Status = CaptureConnecting
			s.view.Metrics.Stage = ListeningStage
		}
	})
}
func (s *Session) saveMeetingConfig(change func(*coreconfig.Config)) error {
	cfg := s.Config()
	change(&cfg)
	if s.saveConfiguration == nil {
		return errors.New(MeetingSaveUnavailable)
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
func (s *Session) UpdateProfile(background, terms string) error {
	background, terms, err := AcceptProfile(background, terms)
	if err != nil {
		return err
	}
	return s.saveMeetingConfig(func(c *coreconfig.Config) {
		c.MeetingBackground = background
		c.MeetingTerms = terms
	})
}
func (s *Session) SetASRMode(mode string) error {
	if !coreconfig.KnownMeetingASRMode(mode) {
		return errors.New(UnknownMeetingASRMode)
	}
	return s.saveMeetingConfig(func(c *coreconfig.Config) { c.MeetingASRMode = mode })
}
func (s *Session) runInference(ctx context.Context, priority int, stage string, call func(context.Context) error) error {
	return s.scheduler.Run(ctx, priority, call, func(running bool) {
		s.mutate(func() {
			if running {
				s.view.Metrics.Stage = stage
			} else {
				_, _, paused := s.runtime.Snapshot()
				if !paused {
					s.view.Metrics.Stage = ListeningStage
				}
			}
			s.view.Metrics.Pending = s.scheduler.Pending()
		})
	})
}

func (s *Session) updateCaption(epoch, id uint64, source, chinese string) {
	s.applyWork(epoch, func() {
		ApplyCaptionTranslation(s.view.Captions, id, source, chinese)
	})
}
func (s *Session) complete(ctx context.Context, priority int, query Query, emit func(Line)) error {
	cfg := s.Config()
	stage := InferenceStage(query.Mode, priority == PriorityExpress)
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
		return Complete(active, cfg, query, func(line Line) {
			if active.Err() == nil {
				emit(line)
			}
		})
	})
}
func (s *Session) queueReplies(job CaptionTask) {
	s.mu.Lock()
	if !s.runtime.Active(job.Epoch) {
		s.mu.Unlock()
		return
	}
	ctx, cancel, ok := s.runtime.ReplyContext(job.Epoch, ReplyTimeout)
	if !ok {
		s.mu.Unlock()
		return
	}
	serial := s.view.Reply.Next()
	s.view.Status = RepliesPreparing
	s.dirty = true
	s.mu.Unlock()
	go func() {
		defer cancel()
		batch := ReplyBatch{Source: job.Latest}
		err := s.complete(ctx, PriorityReply, Query{Mode: "replies", Latest: job.Latest, Prior: job.Prior}, func(line Line) {
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
						s.view.Status = RepliesFailed
					}
				}
			})
		}
	}()
}
func (s *Session) AcceptPendingReplies() {
	s.mutate(func() { s.view.Reply.AcceptPending() })
}
func (s *Session) RequestReplies() {
	s.mu.Lock()
	last, hasCaption := LatestCompletedCaption(s.view.Captions)
	work, epoch, paused := s.runtime.Snapshot()
	if status, start := ReplyRequest(paused, hasCaption); !start {
		if status != "" {
			s.view.Status = status
			s.dirty = true
		}
		s.mu.Unlock()
		return
	}
	prior := ReplyPrior(s.prior, last.Source)
	job := CaptionTask{Latest: last.Source, ID: last.ID, Epoch: epoch, Context: work, Prior: prior, Ended: time.Now()}
	s.mu.Unlock()
	s.queueReplies(job)
}
func (s *Session) RefineReply(index int, instruction string) {
	instruction, ok := AcceptRefine(index, instruction)
	if !ok {
		return
	}
	s.mu.Lock()
	selected := s.view.Reply.Displayed.Lines[index].English
	_, _, paused := s.runtime.Snapshot()
	if selected == "" || paused {
		s.mu.Unlock()
		return
	}
	ctx, cancel, epoch, serial := s.runtime.NewExpression(ReplyTimeout)
	s.view.ExpressionStatus = RefineRunning
	s.dirty = true
	s.mu.Unlock()
	go func() {
		defer cancel()
		err := s.complete(ctx, PriorityExpress, Query{Mode: "refine", SelectedReply: selected, UserPoints: instruction}, func(line Line) {
			s.applyExpression(epoch, serial, func() { s.view.Expression = line; s.view.ExpressionStatus = "" })
		})
		if err != nil && ctx.Err() == nil {
			s.applyExpression(epoch, serial, func() {
				s.view.ExpressionStatus = RefineFailed
				if !s.view.Expression.Complete {
					s.view.Expression = Line{}
				}
			})
		}
	}()
}

func (s *Session) applyExpression(epoch, serial uint64, update func()) {
	s.applyWork(epoch, func() {
		if s.runtime.CurrentExpression(epoch, serial) {
			update()
		}
	})
}

func (s *Session) Start(source AudioSource) {
	s.pipeline = NewPipeline(s.runtime, PipelineHooks{
		Transcribe: func(ctx context.Context, state *ASRState, turn AudioTurn, emit func(string)) (string, error) {
			cfg := s.Config()
			priority := PriorityPreview
			if turn.Final {
				priority = PriorityASR
			}
			var latest string
			err := s.runInference(ctx, priority, CaptionRecognizing, func(active context.Context) error {
				var e error
				latest, e = state.Transcribe(active, cfg, turn.PCM, emit)
				return e
			})
			return latest, err
		},
		Translate: func(ctx context.Context, job CaptionTask, emit func(string)) error {
			return s.complete(ctx, PriorityCaption, Query{Mode: "caption", Prior: job.Prior, Latest: job.Latest}, func(line Line) {
				if line.Kind == "ZH" {
					emit(line.Chinese)
				}
			})
		},
		OnTranscript: func(turn AudioTurn, latest, stable string) []string {
			var prior []string
			s.applyWork(turn.Epoch, func() {
				s.view.Captions = UpsertCaption(s.view.Captions, turn.ID, latest, turn.Final, true)
				if !turn.Final && s.cfg.MeetingASRMode != coreconfig.MeetingASRMultipart {
					SetCaptionPrefix(s.view.Captions, turn.ID, stable)
				}
				prior = append([]string(nil), s.prior...)
				if turn.Final {
					s.prior = RememberPrior(s.prior, latest)
				}
				s.view.Status = ListeningPrompt
			})
			return prior
		},
		OnTranslation: func(job CaptionTask, chinese string) {
			s.updateCaption(job.Epoch, job.ID, job.Latest, chinese)
		},
		OnASRFirst: func(turn AudioTurn, elapsed time.Duration) {
			s.applyWork(turn.Epoch, func() { s.view.Metrics.ASRFirstMS = elapsed.Milliseconds() })
		},
		OnASRDone: func(turn AudioTurn, first, elapsed time.Duration, err error) {
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
		OnTranslationFirst: func(job CaptionTask, elapsed time.Duration) {
			s.applyWork(job.Epoch, func() { s.view.Metrics.TranslationFirstMS = elapsed.Milliseconds() })
			s.logger("meeting stage=caption first_ms=%d since_snapshot_ms=%d", elapsed.Milliseconds(), time.Since(job.Ended).Milliseconds())
		},
		OnTranslationDone: func(job CaptionTask, elapsed time.Duration, err error) {
			s.applyWork(job.Epoch, func() {
				s.view.Metrics.TranslationDoneMS = elapsed.Milliseconds()
				if err != nil {
					s.view.Status = TranslationUnavailable
				}
			})
			if err != nil {
				s.logger("meeting stage=caption error=%v", err)
			}
		},
		OnDropped: func(epoch uint64) {
			s.applyWork(epoch, func() {
				s.view.Metrics.Dropped++
				s.view.Status = DroppedWork
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
			err := StreamAudio(work, source, func() time.Duration {
				s.mu.Lock()
				defer s.mu.Unlock()
				return time.Duration(s.view.Metrics.ASRDoneMS+s.view.Metrics.TranslationDoneMS) * time.Millisecond
			}, func() {
				s.applyWork(epoch, func() { s.view.Status = ListeningPrompt })
			}, func(event AudioEvent) {
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
					s.pipeline.QueueAudio(AudioTurn{
						Final: event.Final, ID: id, Epoch: epoch, Context: work,
						PCM: event.PCM, Ended: event.Time,
					})
				}
			})
			if work.Err() != nil {
				continue
			}
			if err == nil {
				err = errors.New(CaptureEnded)
			}
			s.applyWork(epoch, func() { s.captureErr = err })
			return
		}
	}()
}
