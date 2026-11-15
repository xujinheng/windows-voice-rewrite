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
	activeASRCancel            context.CancelFunc
	activeASRID, latestFinalID uint64
	activeASRFinal             bool
	runtime                    *meetingcore.SessionRuntime
	cfg                        Config
	logger                     func(string, ...any)
	mu                         sync.Mutex
	view                       meetingView
	prior                      []string
	stopReply                  context.CancelFunc
	stopCaption                context.CancelFunc
	captionID                  uint64
	captureErr                 error
	dirty, demo                bool
	modelMu                    sync.Mutex
	model                      string
	scheduler                  meetingcore.Scheduler
	onConfig                   func(Config)
	saveConfiguration          func(Config) error
}

type meetingAudioTurn struct {
	final     bool
	id, epoch uint64
	pcm       []byte
	ended     time.Time
	ctx       context.Context
}
type meetingCaptionJob struct {
	final     bool
	id, epoch uint64
	prior     []string
	latest    string
	ended     time.Time
	ctx       context.Context
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
func (s *meetingSession) workSnapshot() (context.Context, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	work, epoch, _ := s.runtime.Snapshot()
	return work, epoch
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
	if s.stopReply != nil {
		s.stopReply()
	}
	if s.stopCaption != nil {
		s.stopCaption()
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

func (s *meetingSession) transcribeTurns(turns, previews <-chan meetingAudioTurn, captions chan meetingCaptionJob) {
	var state meetingcore.ASRState
	var currentID, currentEpoch, finalized uint64
	for {
		var turn meetingAudioTurn
		select {
		case <-s.runtime.Context().Done():
			return
		case turn = <-turns:
		default:
			select {
			case <-s.runtime.Context().Done():
				return
			case turn = <-turns:
			case turn = <-previews:
			}
		}
		if turn.ctx.Err() != nil || (!turn.final && turn.id <= finalized) {
			continue
		}
		if currentID != turn.id || currentEpoch != turn.epoch {
			state = meetingcore.ASRState{}
			currentID, currentEpoch = turn.id, turn.epoch
		}
		s.transcribeTurn(turn, &state, captions)
		if turn.final {
			finalized = turn.id
		}
	}
}
func (s *meetingSession) transcribeTurn(turn meetingAudioTurn, state *meetingcore.ASRState, captions chan meetingCaptionJob) {
	ctx, cancel := context.WithTimeout(turn.ctx, 16*time.Second)
	defer cancel()
	s.mu.Lock()
	if !turn.final && turn.id <= s.latestFinalID {
		s.mu.Unlock()
		return
	}
	s.activeASRCancel = cancel
	s.activeASRID = turn.id
	s.activeASRFinal = turn.final
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.activeASRCancel = nil; s.mu.Unlock() }()
	cfg := s.configSnapshot()
	priority := meetingcore.PriorityPreview
	if turn.final {
		priority = meetingcore.PriorityASR
	}
	var latest string
	var first time.Duration
	began := time.Now()
	err := s.runInference(ctx, priority, "正在识别", func(active context.Context) error {
		var err error
		latest, err = state.Transcribe(active, cfg, turn.pcm, func(text string) {
			if active.Err() != nil {
				return
			}
			if first == 0 {
				first = time.Since(began)
				s.applyWork(turn.epoch, func() {
					s.view.metrics.ASRFirstMS = first.Milliseconds()
				})
			}
		})
		return err
	})
	if turn.ctx.Err() != nil || errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	elapsed := time.Since(began)
	s.applyWork(turn.epoch, func() { s.view.metrics.ASRDoneMS = elapsed.Milliseconds() })
	if err != nil {
		s.logger("meeting stage=asr error=%v", err)
		s.applyWork(turn.epoch, func() { s.view.status = err.Error() })
		return
	}
	s.logger("meeting stage=asr first_ms=%d complete_ms=%d since_snapshot_ms=%d final=%t", first.Milliseconds(), elapsed.Milliseconds(), time.Since(turn.ended).Milliseconds(), turn.final)
	var job meetingCaptionJob
	s.applyWork(turn.epoch, func() {
		s.view.captions = meetingcore.UpsertCaption(s.view.captions, turn.id, latest, turn.final, true)
		if !turn.final && cfg.MeetingASRMode != "multipart" {
			meetingcore.SetCaptionPrefix(s.view.captions, turn.id, state.StablePrefix())
		}
		job = meetingCaptionJob{final: turn.final, id: turn.id, epoch: turn.epoch, prior: append([]string(nil), s.prior...), latest: latest, ended: turn.ended, ctx: turn.ctx}
		if turn.final {
			s.prior = append(s.prior, latest)
			if len(s.prior) > 5 {
				s.prior = s.prior[len(s.prior)-5:]
			}
		}
		s.view.status = "正在聆听 · 按住听写键说中文"
		// An obsolete translation of the same utterance must not hold up its final.
		if s.stopCaption != nil && s.captionID == turn.id {
			s.stopCaption()
		}
	})
	if job.ctx == nil {
		return
	}
	select {
	case captions <- job:
	default:
		select {
		case <-captions:
			s.applyWork(turn.epoch, func() { s.view.metrics.Dropped++ })
		default:
		}
		select {
		case captions <- job:
		default:
		}
	}
}

func (s *meetingSession) updateCaption(epoch, id uint64, source, chinese string) {
	if !meetingcore.TranslationFitsSource(source, chinese) {
		s.logger("meeting stage=caption discarded overlong translation for source %d bytes", len(source))
		return
	}
	s.applyWork(epoch, func() {
		meetingcore.ApplyCaptionTranslation(s.view.captions, id, source, chinese)
	})
}
func (s *meetingSession) translateTurns(jobs <-chan meetingCaptionJob) {
	for {
		select {
		case <-s.runtime.Context().Done():
			return
		case job := <-jobs:
			if job.ctx.Err() != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(job.ctx, 16*time.Second)
			s.applyWork(job.epoch, func() { s.stopCaption = cancel; s.captionID = job.id })
			var displayed bool
			started := time.Now()
			err := s.complete(ctx, meetingcore.PriorityCaption, meetingcore.Query{Mode: "caption", Prior: job.prior, Latest: job.latest}, func(line meetingLine) {
				if line.kind != "ZH" {
					return
				}
				s.updateCaption(job.epoch, job.id, job.latest, line.chinese)
				if !displayed {
					s.applyWork(job.epoch, func() { s.view.metrics.TranslationFirstMS = time.Since(started).Milliseconds() })
					s.logger("meeting stage=caption first_ms=%d since_snapshot_ms=%d", time.Since(started).Milliseconds(), time.Since(job.ended).Milliseconds())
				}
				displayed = true
			})
			s.applyWork(job.epoch, func() { s.view.metrics.TranslationDoneMS = time.Since(started).Milliseconds() })
			if err != nil && ctx.Err() == nil {
				s.logger("meeting stage=caption error=%v", err)
				// Failure must not destroy a readable translation from a
				// previous completed snapshot. The original ASR stays visible.
				s.applyWork(job.epoch, func() { s.view.status = "翻译暂不可用 · 原文已保留" })
			}
			cancel()
		}
	}
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
func (s *meetingSession) queueReplies(job meetingCaptionJob) {
	s.mu.Lock()
	if !s.runtime.Active(job.epoch) {
		s.mu.Unlock()
		return
	}
	if s.stopReply != nil {
		s.stopReply()
	}
	serial := s.view.reply.Next()
	work, _, _ := s.runtime.Snapshot()
	ctx, cancel := context.WithTimeout(work, 30*time.Second)
	s.stopReply = cancel
	s.view.status = "正在准备回复建议…"
	s.dirty = true
	s.mu.Unlock()
	go func() {
		defer cancel()
		batch := meetingcore.ReplyBatch{Source: job.latest}
		err := s.complete(ctx, meetingcore.PriorityReply, meetingcore.Query{Mode: "replies", Latest: job.latest, Prior: job.prior}, func(line meetingLine) {
			if !batch.Apply(meetingcore.Line{Kind: line.kind, English: line.english, Chinese: line.chinese, Complete: line.complete}) {
				return
			}
			// Epoch, generation, and context are checked under the session
			// lock before partial output reaches the HUD.
			s.applyWork(job.epoch, func() {
				if ctx.Err() == nil {
					s.view.reply.Publish(serial, batch)
				}
			})
		})
		if err != nil {
			s.applyWork(job.epoch, func() {
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
	job := meetingCaptionJob{latest: last.Source, id: last.ID, epoch: epoch, ctx: work, prior: prior, ended: time.Now()}
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
	ctx, cancel, epoch, serial := s.expressionContextLocked(30 * time.Second)
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

func (s *meetingSession) queueAudio(queue chan meetingAudioTurn, job meetingAudioTurn) {
	if job.final {
		s.mu.Lock()
		s.latestFinalID = max(s.latestFinalID, job.id)
		if s.activeASRCancel != nil && !s.activeASRFinal && s.activeASRID <= job.id {
			s.activeASRCancel()
		}
		s.mu.Unlock()
	}

	select {
	case queue <- job:
		return
	default:
	}
	select {
	case <-queue:
		s.applyWork(job.epoch, func() {
			s.view.metrics.Dropped++
			s.view.status = "处理较慢 · 已跳过过期任务，正在跟上最新语音"
		})
	default:
	}
	select {
	case queue <- job:
	default:
	}
}
func (s *meetingSession) applyExpression(epoch, serial uint64, update func()) {
	s.applyWork(epoch, func() {
		if s.runtime.CurrentExpression(epoch, serial) {
			update()
		}
	})
}

func (s *meetingSession) start(source audio.FrameSource) {
	turns := make(chan meetingAudioTurn, 2)
	previews := make(chan meetingAudioTurn, 1)
	captions := make(chan meetingCaptionJob, 1)
	go s.transcribeTurns(turns, previews, captions)
	go s.translateTurns(captions)
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
					pending := len(turns) + len(previews) + len(captions) + s.scheduler.Pending()
					s.applyWork(epoch, func() { s.view.level = event.Level; s.view.metrics.Pending = pending })
				}
				if event.UtteranceStarted {
					s.applyWork(epoch, func() {
						id, _ = s.runtime.NextTurn(epoch)
						s.view.reply.DiscardPartial()
						s.view.reply.Next()
						if s.stopReply != nil {
							s.stopReply()
						}
					})
				}
				if event.PCM != nil {
					job := meetingAudioTurn{final: event.Final, id: id, epoch: epoch, ctx: work, pcm: event.PCM, ended: event.Time}
					if event.Final {
						s.queueAudio(turns, job)
					} else {
						s.queueAudio(previews, job)
					}
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
