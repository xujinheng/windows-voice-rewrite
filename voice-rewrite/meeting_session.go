package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"voicerewrite/internal/core/audio"
	meetingcore "voicerewrite/internal/core/meeting"
)

type meetingSession struct {
	expressionSerial             uint64
	stopExpression               context.CancelFunc
	activeASRCancel              context.CancelFunc
	activeASRID, latestFinalID   uint64
	activeASRFinal               bool
	ctx                          context.Context
	stop                         context.CancelFunc
	cfg                          Config
	logger                       func(string, ...any)
	mu                           sync.Mutex
	view                         meetingView
	prior                        []string
	turnID, epoch, replySerial   uint64
	displaySerial, pendingSerial uint64
	work                         context.Context
	cancelWork                   context.CancelFunc
	wake                         chan struct{}
	stopReply                    context.CancelFunc
	stopCaption                  context.CancelFunc
	captionID                    uint64
	captureErr                   error
	dirty, demo                  bool
	modelMu                      sync.Mutex
	model                        string
	scheduler                    meetingcore.Scheduler
	lastVoice                    time.Time
	onConfig                     func(Config)
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

func newMeetingCore(cfg Config, logger func(string, ...any)) *meetingSession {
	ctx, stop := context.WithCancel(context.Background())
	work, cancel := context.WithCancel(ctx)
	return &meetingSession{ctx: ctx, stop: stop, cfg: cfg, logger: logger, work: work, cancelWork: cancel, wake: make(chan struct{}, 1), epoch: 1}
}
func (s *meetingSession) mutate(update func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	update()
	s.dirty = true
}
func (s *meetingSession) applyWork(epoch uint64, update func()) {
	s.mutate(func() {
		if epoch == s.epoch && !s.view.capturePaused {
			update()
		}
	})
}
func (s *meetingSession) workSnapshot() (context.Context, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.work, s.epoch
}
func (s *meetingSession) configSnapshot() Config { s.mu.Lock(); defer s.mu.Unlock(); return s.cfg }
func (s *meetingSession) resetWorkLocked() {
	s.cancelWork()
	s.discardPartialLocked()
	if !s.view.expression.complete {
		s.view.expression = meetingLine{}
	}
	kept := s.view.captions[:0]
	for _, c := range s.view.captions {
		if c.final {
			kept = append(kept, c)
		}
	}
	s.view.captions = kept
	s.view.pendingReplies = [3]meetingLine{}
	s.view.replyPending = false
	if s.view.expressionStatus != "" {
		s.view.expressionStatus = "处理已取消"
	}
	s.epoch++
	s.turnID++
	s.replySerial++
	if s.stopReply != nil {
		s.stopReply()
	}
	if s.stopCaption != nil {
		s.stopCaption()
	}
	s.work, s.cancelWork = context.WithCancel(s.ctx)
	s.view.level = 0
	s.view.metrics.Pending = 0
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *meetingSession) setCapturePaused(paused bool) {
	s.mutate(func() {
		if s.view.capturePaused == paused {
			return
		}
		s.view.capturePaused = paused
		s.resetWorkLocked()
		if paused {
			s.cancelWork()
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
	if err := saveConfig(cfg); err != nil {
		return err
	}
	s.mutate(func() {
		s.cfg = cfg
		s.resetWorkLocked()
		s.view.asrMode = cfg.MeetingASRMode
		if s.view.capturePaused {
			s.cancelWork()
		}
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
func meetingPreviewInterval(elapsed time.Duration) time.Duration {
	return max(800*time.Millisecond, min(2400*time.Millisecond, elapsed*5/4))
}

func (s *meetingSession) runInference(ctx context.Context, priority int, stage string, call func(context.Context) error) error {
	for {
		active, release, err := s.scheduler.Acquire(ctx, priority)
		if err != nil {
			return err
		}
		s.mutate(func() { s.view.metrics.Stage = stage; s.view.metrics.Pending = s.scheduler.Pending() })
		err = call(active)
		preempted := errors.Is(context.Cause(active), meetingcore.ErrPreempted)
		if !preempted && ctx.Err() == nil {
			s.mutate(func() {
				if !s.view.capturePaused {
					s.view.metrics.Stage = "正在听"
					s.view.metrics.Pending = s.scheduler.Pending()
				}
			})
		}
		release()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if preempted && priority == meetingcore.PriorityReply {
			return meetingcore.ErrPreempted
		}
		if preempted {
			continue
		}
		s.mutate(func() { s.view.metrics.Stage = "正在听"; s.view.metrics.Pending = s.scheduler.Pending() })
		return err
	}
}

func (s *meetingSession) transcribeTurns(turns, previews <-chan meetingAudioTurn, captions chan meetingCaptionJob) {
	var state meetingASRState
	var currentID, currentEpoch, finalized uint64
	for {
		var turn meetingAudioTurn
		select {
		case <-s.ctx.Done():
			return
		case turn = <-turns:
		default:
			select {
			case <-s.ctx.Done():
				return
			case turn = <-turns:
			case turn = <-previews:
			}
		}
		if turn.ctx.Err() != nil || (!turn.final && turn.id <= finalized) {
			continue
		}
		if currentID != turn.id || currentEpoch != turn.epoch {
			state = meetingASRState{}
			currentID, currentEpoch = turn.id, turn.epoch
		}
		s.transcribeTurn(turn, &state, captions)
		if turn.final {
			finalized = turn.id
		}
	}
}
func (s *meetingSession) transcribeTurn(turn meetingAudioTurn, state *meetingASRState, captions chan meetingCaptionJob) {
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
		latest, err = state.transcribe(active, cfg, turn.pcm, func(text string) {
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
		s.upsertSourceLocked(turn.id, latest, turn.final, true)
		if !turn.final && cfg.MeetingASRMode != "multipart" {
			s.setSourcePrefixLocked(turn.id, state.fixed)
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

func (s *meetingSession) upsertSourceLocked(id uint64, text string, final, confirmed bool) {
	if strings.TrimSpace(text) == "" {
		return
	}
	for i := range s.view.captions {
		old := s.view.captions[i]
		if old.id != id {
			continue
		}
		if old.final {
			return
		}
		if confirmed {
			s.view.captions[i] = reviseMeetingCaption(old, id, text, final)
		} else {
			old.source = text
			old.live = strings.TrimPrefix(text, old.stable)
			if !strings.HasPrefix(text, old.stable) {
				old.stable = ""
				old.live = text
			}
			s.view.captions[i] = old
		}
		return
	}
	s.view.captions = append(s.view.captions, reviseMeetingCaption(meetingCaption{}, id, text, final))
}
func (s *meetingSession) updateCaption(epoch, id uint64, source, chinese string) {
	if !meetingcore.TranslationFitsSource(source, chinese) {
		s.logger("meeting stage=caption discarded overlong translation for source %d bytes", len(source))
		return
	}
	s.applyWork(epoch, func() {
		for i := range s.view.captions {
			line := &s.view.captions[i]
			if line.id == id && line.source == source {
				line.chinese = chinese
				line.translatedFrom = source
				return
			}
		}
	})
}
func (s *meetingSession) translateTurns(jobs <-chan meetingCaptionJob) {
	for {
		select {
		case <-s.ctx.Done():
			return
		case job := <-jobs:
			if job.ctx.Err() != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(job.ctx, 16*time.Second)
			s.applyWork(job.epoch, func() { s.stopCaption = cancel; s.captionID = job.id })
			var displayed bool
			started := time.Now()
			err := s.complete(ctx, meetingcore.PriorityCaption, meetingQuery{Mode: "caption", Prior: job.prior, Latest: job.latest}, func(line meetingLine) {
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
func (s *meetingSession) complete(ctx context.Context, priority int, query meetingQuery, emit func(meetingLine)) error {
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
			model, err := discoverModel(active, cfg)
			if err != nil {
				s.modelMu.Unlock()
				return err
			}
			s.model = model
		}
		cfg.TextModel = s.model
		s.modelMu.Unlock()
		return completeMeeting(active, cfg, query, func(line meetingLine) {
			if active.Err() == nil {
				emit(line)
			}
		})
	})
}
func (s *meetingSession) queueReplies(job meetingCaptionJob) {
	s.mu.Lock()
	if s.ctx.Err() != nil || s.view.capturePaused || job.epoch != s.epoch {
		s.mu.Unlock()
		return
	}
	if s.stopReply != nil {
		s.stopReply()
	}
	s.replySerial++
	serial := s.replySerial
	ctx, cancel := context.WithTimeout(s.work, 30*time.Second)
	s.stopReply = cancel
	s.view.status = "正在准备回复建议…"
	s.dirty = true
	s.mu.Unlock()
	go func() {
		defer cancel()
		var batch [3]meetingLine
		tip := ""
		err := s.complete(ctx, meetingcore.PriorityReply, meetingQuery{Mode: "replies", Latest: job.latest, Prior: job.prior}, func(line meetingLine) {
			switch line.kind {
			case "ONE":
				batch[0] = line
			case "TWO":
				batch[1] = line
			case "THREE":
				batch[2] = line
			case "TIP":
				tip = line.chinese
			}
			s.mu.Lock()
			valid := serial == s.replySerial && ctx.Err() == nil
			s.mu.Unlock()
			if valid {
				s.publishRepliesFor(job.epoch, serial, batch, job.latest, tip)
			}
		})
		if err != nil {
			s.applyWork(job.epoch, func() {
				if serial == s.replySerial {
					s.discardPartialLocked()
				}
			})
		}
		if err != nil && ctx.Err() == nil {
			s.applyWork(job.epoch, func() {
				if serial == s.replySerial {
					s.view.status = "建议生成失败 · 保留当前回复"
				}
			})
		}
	}()
}
func (s *meetingSession) publishRepliesFor(epoch, serial uint64, batch [3]meetingLine, source, tip string) {
	s.applyWork(epoch, func() {
		if serial != s.replySerial {
			return
		}
		s.publishRepliesLocked(serial, batch, source, tip)
	})
}
func (s *meetingSession) publishRepliesLocked(serial uint64, batch [3]meetingLine, source, tip string) {
	if s.view.replies[0].english == "" || s.displaySerial == serial {
		s.displaySerial = serial
		s.view.replies = batch
		s.view.replySource = source
		s.view.tip = tip
		return
	}
	s.pendingSerial = serial
	s.view.pendingReplies = batch
	s.view.pendingReplySource = source
	s.view.pendingTip = tip
	s.view.replyPending = batch[0].complete
}
func (s *meetingSession) publishReplies(batch [3]meetingLine, source, tip string) {
	s.mutate(func() { s.replySerial++; s.publishRepliesLocked(s.replySerial, batch, source, tip) })
}
func (s *meetingSession) acceptPendingReplies() {
	s.mutate(func() {
		if !s.view.replyPending {
			return
		}
		s.displaySerial = s.pendingSerial
		s.view.replies = s.view.pendingReplies
		s.view.replySource = s.view.pendingReplySource
		s.view.tip = s.view.pendingTip
		s.view.replyPending = false
		s.view.pendingReplies = [3]meetingLine{}
	})
}
func (s *meetingSession) requestReplies() {
	s.mu.Lock()
	if s.view.capturePaused {
		s.mu.Unlock()
		return
	}
	last, ok := meetingLastCompletedCaption(s.view.captions)
	if !ok {
		s.view.status = "请等当前发言结束后再生成建议"
		s.dirty = true
		s.mu.Unlock()
		return
	}
	prior := append([]string(nil), s.prior...)
	if len(prior) > 0 && strings.TrimSpace(prior[len(prior)-1]) == strings.TrimSpace(last.source) {
		prior = prior[:len(prior)-1]
	}
	job := meetingCaptionJob{latest: last.source, id: last.id, epoch: s.epoch, ctx: s.work, prior: prior, ended: time.Now()}
	s.mu.Unlock()
	s.queueReplies(job)
}
func (s *meetingSession) refineReply(index int, instruction string) {
	instruction = strings.TrimSpace(instruction)
	if index < 0 || index > 2 || instruction == "" || len([]rune(instruction)) > 2000 {
		return
	}
	s.mu.Lock()
	selected := s.view.replies[index].english
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
		err := s.complete(ctx, meetingcore.PriorityExpress, meetingQuery{Mode: "refine", SelectedReply: selected, UserPoints: instruction}, func(line meetingLine) {
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
func (s *meetingSession) setSourcePrefixLocked(id uint64, prefix string) {
	for i := range s.view.captions {
		c := &s.view.captions[i]
		if c.id == id && !c.final && strings.HasPrefix(c.source, prefix) {
			c.stable = prefix
			c.live = strings.TrimPrefix(c.source, prefix)
			return
		}
	}
}

func (s *meetingSession) discardPartialLocked() {
	for i := range s.view.replies {
		if !s.view.replies[i].complete {
			s.view.replies[i] = meetingLine{}
		}
	}
	for i := range s.view.pendingReplies {
		if !s.view.pendingReplies[i].complete {
			s.view.pendingReplies[i] = meetingLine{}
		}
	}
	s.view.replyPending = s.view.pendingReplies[0].complete
}

func (s *meetingSession) expressionContextLocked(timeout time.Duration) (context.Context, context.CancelFunc, uint64, uint64) {
	if s.stopExpression != nil {
		s.stopExpression()
	}
	s.expressionSerial++
	ctx, cancel := context.WithTimeout(s.work, timeout)
	s.stopExpression = cancel
	return ctx, cancel, s.epoch, s.expressionSerial
}
func (s *meetingSession) applyExpression(epoch, serial uint64, update func()) {
	s.applyWork(epoch, func() {
		if serial == s.expressionSerial {
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
		for s.ctx.Err() == nil {
			s.mu.Lock()
			paused := s.view.capturePaused
			work, epoch := s.work, s.epoch
			s.mu.Unlock()
			if paused {
				select {
				case <-s.ctx.Done():
					return
				case <-s.wake:
					continue
				}
			}
			var seg audio.Segmenter
			var id uint64
			var lastMeter time.Time
			err := source.Stream(work, func() { s.applyWork(epoch, func() { s.view.status = "正在聆听 · 按住听写键说中文" }) }, func(frame []byte) {
				if work.Err() != nil {
					return
				}
				now := time.Now()
				level := audio.Level(frame)
				s.mu.Lock()
				if level >= 4 {
					s.lastVoice = now
				}
				elapsed := time.Duration(s.view.metrics.ASRDoneMS+s.view.metrics.TranslationDoneMS) * time.Millisecond
				s.mu.Unlock()
				seg.SetPreviewInterval(meetingPreviewInterval(elapsed))
				if now.Sub(lastMeter) >= 150*time.Millisecond {
					lastMeter = now
					pending := len(turns) + len(previews) + len(captions) + s.scheduler.Pending()
					s.applyWork(epoch, func() { s.view.level = level; s.view.metrics.Pending = pending })
				}
				started, pcm := seg.Push(frame)
				if started {
					s.applyWork(epoch, func() {
						s.turnID++
						id = s.turnID
						s.replySerial++
						s.discardPartialLocked()
						if s.stopReply != nil {
							s.stopReply()
						}
					})
				}
				final := pcm != nil
				if !final {
					pcm = seg.Preview()
				}
				if pcm != nil {
					job := meetingAudioTurn{final: final, id: id, epoch: epoch, ctx: work, pcm: pcm, ended: now}
					if final {
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
