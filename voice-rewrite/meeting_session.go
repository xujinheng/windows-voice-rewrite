package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"voicerewrite/internal/core/audio"
	meetingcore "voicerewrite/internal/core/meeting"
	"voicerewrite/internal/core/provider"
)

type meetingSession struct {
	pipeline                   *meetingcore.Pipeline
	runtime                    *meetingcore.SessionRuntime
	cfg                        Config
	logger                     func(string, ...any)
	mu                         sync.Mutex
	view                       meetingView
	prior                      []string
	captureErr                 error
	dirty, demo                bool
	modelMu                    sync.Mutex
	model                      string
	scheduler                  meetingcore.Scheduler
	onConfig                   func(Config)
	saveConfiguration          func(Config) error
}

func newMeetingCore(cfg Config, logger func(string, ...any), saveConfiguration func(Config) error) *meetingSession {
	return &meetingSession{runtime: meetingcore.NewSessionRuntime(), cfg: cfg, logger: logger, saveConfiguration: saveConfiguration}
}
func (s *meetingSession) mutate(update func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtime.Context().Err() != nil {
		return
	}
	update()
	s.dirty = true
}
func (s *meetingSession) applyWork(epoch uint64, update func()) {
	s.mutate(func() {
		if s.runtime.Active(epoch) {
			update()
		}
	})
}
func (s *meetingSession) configSnapshot() Config { s.mu.Lock(); defer s.mu.Unlock(); return s.cfg }

// resetViewLocked runs with the host state mutex held after the runtime
// invalidates its epoch. Presentation never survives a canceled partial task.
func (s *meetingSession) resetViewLocked() {
	s.view.reply.ResetWork()
	if !s.view.expression.complete {
		s.view.expression = meetingLine{}
	}
	s.view.captions = meetingcore.KeepCompletedCaptions(s.view.captions)
	if s.view.expressionStatus != "" {
		s.view.expressionStatus = "处理已取消"
	}
	s.view.level = 0
	s.view.metrics.Pending = 0
}

func (s *meetingSession) resetWorkLocked() {
	s.runtime.ResetWork()
	s.resetViewLocked()
}
func (s *meetingSession) setCapturePaused(paused bool) {
	s.mutate(func() {
		if !s.runtime.SetPaused(paused) {
			return
		}
		s.view.capturePaused = paused
		s.resetViewLocked()
		if paused {
			s.view.status = "采集已暂停 · 不发送音频"
			s.view.metrics.Stage = "已暂停"
		} else {
			s.view.status = "正在连接音频…"
			s.view.metrics.Stage = "正在听"
		}
	})
}
func (s *meetingSession) saveMeetingConfig(change func(*Config)) error {
	cfg := s.configSnapshot()
	change(&cfg)
	if s.saveConfiguration == nil {
		return errors.New("未配置会议设置保存服务")
	}
	if err := s.saveConfiguration(cfg); err != nil {
		return err
	}
	s.mutate(func() {
		s.cfg = cfg
		s.resetWorkLocked()
		s.view.asrMode = cfg.MeetingASRMode
	})
	if s.onConfig != nil {
		s.onConfig(cfg)
	}
	return nil
}
func (s *meetingSession) updateProfile(background, terms string) error {
	if len([]rune(background)) > 4000 || len([]rune(terms)) > 2000 {
		return errors.New("背景最多 4000 字，术语最多 2000 字")
	}
	return s.saveMeetingConfig(func(c *Config) {
		c.MeetingBackground = strings.TrimSpace(background)
		c.MeetingTerms = strings.TrimSpace(terms)
	})
}
func (s *meetingSession) setASRMode(mode string) error {
	if mode != "llama-prefix" && mode != "multipart" {
		return errors.New("无效的 ASR 模式")
	}
	return s.saveMeetingConfig(func(c *Config) { c.MeetingASRMode = mode })
}
func (s *meetingSession) runInference(ctx context.Context, priority int, stage string, call func(context.Context) error) error {
	return s.scheduler.Run(ctx, priority, call, func(running bool) {
		s.mutate(func() {
			if running {
				s.view.metrics.Stage = stage
			} else if !s.view.capturePaused {
				s.view.metrics.Stage = "正在听"
			}
			s.view.metrics.Pending = s.scheduler.Pending()
		})
	})
}

func (s *meetingSession) updateCaption(epoch, id uint64, source, chinese string) {
	s.applyWork(epoch, func() {
		meetingcore.ApplyCaptionTranslation(s.view.captions, id, source, chinese)
	})
}
func (s *meetingSession) complete(ctx context.Context, priority int, query meetingcore.Query, emit func(meetingLine)) error {
	cfg := s.configSnapshot()
	stage := "正在翻译"
	if query.Mode == "replies" {
		stage = "正在准备建议"
	}
	if priority == meetingcore.PriorityExpress {
		stage = "正在整理你的表达"
	}
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
				emit(meetingLine{kind: line.Kind, english: line.English, chinese: line.Chinese, complete: line.Complete})
			}
		})
	})
}
func (s *meetingSession) queueReplies(job meetingcore.CaptionTask) {
	s.mu.Lock()
	if !s.runtime.Active(job.Epoch) {
		s.mu.Unlock()
		return
	}
	ctx, cancel, ok := s.runtime.ReplyContext(job.Epoch, 30*time.Second)
	if !ok {
		s.mu.Unlock()
		return
	}
	serial := s.view.reply.Next()
	s.view.status = "正在准备回复建议…"
	s.dirty = true
	s.mu.Unlock()
	go func() {
		defer cancel()
		batch := meetingcore.ReplyBatch{Source: job.Latest}
		err := s.complete(ctx, meetingcore.PriorityReply, meetingcore.Query{Mode: "replies", Latest: job.Latest, Prior: job.Prior}, func(line meetingLine) {
			if !batch.Apply(meetingcore.Line{Kind: line.kind, English: line.english, Chinese: line.chinese, Complete: line.complete}) {
				return
			}
			// Epoch, generation, and context are checked under the session
			// lock before partial output reaches the HUD.
			s.applyWork(job.Epoch, func() {
				if ctx.Err() == nil {
					s.view.reply.Publish(serial, batch)
				}
			})
		})
		if err != nil {
			s.applyWork(job.Epoch, func() {
				if s.view.reply.Current(serial) {
					s.view.reply.DiscardPartial()
					if ctx.Err() == nil {
						s.view.status = "建议生成失败 · 保留当前回复"
					}
				}
			})
		}
	}()
}
func (s *meetingSession) acceptPendingReplies() {
	s.mutate(func() { s.view.reply.AcceptPending() })
}
func (s *meetingSession) requestReplies() {
	s.mu.Lock()
	if s.view.capturePaused {
		s.mu.Unlock()
		return
	}
	last, ok := meetingcore.LatestCompletedCaption(s.view.captions)
	if !ok {
		s.view.status = "请等当前发言结束后再生成建议"
		s.dirty = true
		s.mu.Unlock()
		return
	}
	prior := append([]string(nil), s.prior...)
	if len(prior) > 0 && strings.TrimSpace(prior[len(prior)-1]) == strings.TrimSpace(last.Source) {
		prior = prior[:len(prior)-1]
	}
	work, epoch, _ := s.runtime.Snapshot()
	job := meetingcore.CaptionTask{Latest: last.Source, ID: last.ID, Epoch: epoch, Context: work, Prior: prior, Ended: time.Now()}
	s.mu.Unlock()
	s.queueReplies(job)
}
func (s *meetingSession) refineReply(index int, instruction string) {
	instruction = strings.TrimSpace(instruction)
	if index < 0 || index > 2 || instruction == "" || len([]rune(instruction)) > 2000 {
		return
	}
	s.mu.Lock()
	selected := s.view.reply.Displayed.Lines[index].English
	if selected == "" || s.view.capturePaused {
		s.mu.Unlock()
		return
	}
	ctx, cancel, epoch, serial := s.runtime.NewExpression(30 * time.Second)
	s.view.expressionStatus = "正在微调所选回复…"
	s.dirty = true
	s.mu.Unlock()
	go func() {
		defer cancel()
		err := s.complete(ctx, meetingcore.PriorityExpress, meetingcore.Query{Mode: "refine", SelectedReply: selected, UserPoints: instruction}, func(line meetingLine) {
			s.applyExpression(epoch, serial, func() { s.view.expression = line; s.view.expressionStatus = "" })
		})
		if err != nil && ctx.Err() == nil {
			s.applyExpression(epoch, serial, func() {
				s.view.expressionStatus = "微调失败，请重试"
				if !s.view.expression.complete {
					s.view.expression = meetingLine{}
				}
			})
		}
	}()
}

func (s *meetingSession) applyExpression(epoch, serial uint64, update func()) {
	s.applyWork(epoch, func() {
		if s.runtime.CurrentExpression(epoch, serial) {
			update()
		}
	})
}

func (s *meetingSession) start(source audio.FrameSource) {
	s.pipeline = meetingcore.NewPipeline(s.runtime, meetingcore.PipelineHooks{
		Transcribe: func(ctx context.Context, state *meetingcore.ASRState, turn meetingcore.AudioTurn, emit func(string)) (string, error) {
			cfg := s.configSnapshot()
			priority := meetingcore.PriorityPreview
			if turn.Final {
				priority = meetingcore.PriorityASR
			}
			var latest string
			err := s.runInference(ctx, priority, "正在识别", func(active context.Context) error {
				var e error
				latest, e = state.Transcribe(active, cfg, turn.PCM, emit)
				return e
			})
			return latest, err
		},
		Translate: func(ctx context.Context, job meetingcore.CaptionTask, emit func(string)) error {
			return s.complete(ctx, meetingcore.PriorityCaption, meetingcore.Query{Mode: "caption", Prior: job.Prior, Latest: job.Latest}, func(line meetingLine) {
				if line.kind == "ZH" {
					emit(line.chinese)
				}
			})
		},
		OnTranscript: func(turn meetingcore.AudioTurn, latest, stable string) []string {
			var prior []string
			s.applyWork(turn.Epoch, func() {
				s.view.captions = meetingcore.UpsertCaption(s.view.captions, turn.ID, latest, turn.Final, true)
				if !turn.Final && s.cfg.MeetingASRMode != "multipart" {
					meetingcore.SetCaptionPrefix(s.view.captions, turn.ID, stable)
				}
				prior = append([]string(nil), s.prior...)
				if turn.Final {
					s.prior = append(s.prior, latest)
					if len(s.prior) > 5 {
						s.prior = s.prior[len(s.prior)-5:]
					}
				}
				s.view.status = "正在聆听 · 按住听写键说中文"
			})
			return prior
		},
		OnTranslation: func(job meetingcore.CaptionTask, chinese string) {
			s.updateCaption(job.Epoch, job.ID, job.Latest, chinese)
		},
		OnASRFirst: func(turn meetingcore.AudioTurn, elapsed time.Duration) {
			s.applyWork(turn.Epoch, func() { s.view.metrics.ASRFirstMS = elapsed.Milliseconds() })
		},
		OnASRDone: func(turn meetingcore.AudioTurn, first, elapsed time.Duration, err error) {
			s.applyWork(turn.Epoch, func() {
				s.view.metrics.ASRDoneMS = elapsed.Milliseconds()
				if err != nil {
					s.view.status = err.Error()
				}
			})
			if err != nil {
				s.logger("meeting stage=asr error=%v", err)
			} else {
				s.logger("meeting stage=asr first_ms=%d complete_ms=%d since_snapshot_ms=%d final=%t", first.Milliseconds(), elapsed.Milliseconds(), time.Since(turn.Ended).Milliseconds(), turn.Final)
			}
		},
		OnTranslationFirst: func(job meetingcore.CaptionTask, elapsed time.Duration) {
			s.applyWork(job.Epoch, func() { s.view.metrics.TranslationFirstMS = elapsed.Milliseconds() })
			s.logger("meeting stage=caption first_ms=%d since_snapshot_ms=%d", elapsed.Milliseconds(), time.Since(job.Ended).Milliseconds())
		},
		OnTranslationDone: func(job meetingcore.CaptionTask, elapsed time.Duration, err error) {
			s.applyWork(job.Epoch, func() {
				s.view.metrics.TranslationDoneMS = elapsed.Milliseconds()
				if err != nil {
					s.view.status = "翻译暂不可用 · 原文已保留"
				}
			})
			if err != nil {
				s.logger("meeting stage=caption error=%v", err)
			}
		},
		OnDropped: func(epoch uint64) {
			s.applyWork(epoch, func() {
				s.view.metrics.Dropped++
				s.view.status = "处理较慢 · 已跳过过期任务，正在跟上最新语音"
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
				return time.Duration(s.view.metrics.ASRDoneMS+s.view.metrics.TranslationDoneMS) * time.Millisecond
			}, func() {
				s.applyWork(epoch, func() { s.view.status = "正在聆听 · 按住听写键说中文" })
			}, func(event meetingcore.AudioEvent) {
				if event.Meter {
					pending := s.pipeline.Pending() + s.scheduler.Pending()
					s.applyWork(epoch, func() { s.view.level = event.Level; s.view.metrics.Pending = pending })
				}
				if event.UtteranceStarted {
					s.applyWork(epoch, func() {
						id, _ = s.runtime.NextTurn(epoch)
						s.view.reply.DiscardPartial()
						s.view.reply.Next()
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
				err = errors.New("音频采集意外结束")
			}
			s.applyWork(epoch, func() { s.captureErr = err })
			return
		}
	}()
}
