package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
	id       string
	runtime  *meetingcore.SessionRuntime
	pipeline *meetingcore.Pipeline
	source   *meetingPacketSource

	mu       sync.Mutex
	captions []meetingcore.Caption
	prior    []string
	status   string
	level    int
}

func newBridgeMeeting(cfg coreconfig.Config) (*bridgeMeeting, error) {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, err
	}
	m := &bridgeMeeting{
		id:      hex.EncodeToString(key[:]),
		runtime: meetingcore.NewSessionRuntime(),
		source:  &meetingPacketSource{packets: make(chan []byte, 12)},
		status:  "正在监听应用音频",
	}
	m.pipeline = meetingcore.NewPipeline(m.runtime, meetingcore.PipelineHooks{
		Transcribe: func(ctx context.Context, state *meetingcore.ASRState, turn meetingcore.AudioTurn, emit func(string)) (string, error) {
			return state.Transcribe(ctx, cfg, turn.PCM, emit)
		},
		Translate: func(ctx context.Context, job meetingcore.CaptionTask, emit func(string)) error {
			return meetingcore.Complete(ctx, cfg, meetingcore.Query{
				Mode: "caption", Latest: job.Latest, Prior: job.Prior,
			}, func(line meetingcore.Line) {
				if line.Kind == "ZH" {
					emit(line.Chinese)
				}
			})
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
			}
			m.status = "正在监听应用音频"
			return prior
		},
		OnTranslation: func(job meetingcore.CaptionTask, chinese string) {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.runtime.Active(job.Epoch) {
				meetingcore.ApplyCaptionTranslation(m.captions, job.ID, job.Latest, chinese)
			}
		},
		OnASRDone: func(turn meetingcore.AudioTurn, _, _ time.Duration, err error) {
			if err != nil {
				m.mu.Lock()
				if m.runtime.Active(turn.Epoch) {
					m.status = "会议识别失败，请检查 ASR 服务与模型"
				}
				m.mu.Unlock()
			}
		},
		OnTranslationDone: func(job meetingcore.CaptionTask, _ time.Duration, err error) {
			if err != nil {
				m.mu.Lock()
				if m.runtime.Active(job.Epoch) {
					m.status = "翻译服务不可用；原文仍可阅读"
				}
				m.mu.Unlock()
			}
		},
	})
	m.pipeline.Run()
	go func() {
		work, epoch, _ := m.runtime.Snapshot()
		var turnID uint64
		_ = meetingcore.StreamAudio(work, m.source, nil, func() {}, func(event meetingcore.AudioEvent) {
			if event.Meter {
				m.mu.Lock()
				m.level = event.Level
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
	}()
	return m, nil
}

var errMeetingBusy = errors.New("meeting audio queue full")

func (m *bridgeMeeting) feed(pcm []byte) error {
	if m.runtime.Context().Err() != nil {
		return context.Canceled
	}
	select {
	case m.source.packets <- pcm:
		return nil
	default:
		return errMeetingBusy
	}
}

func (m *bridgeMeeting) stop() { m.runtime.Stop() }

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
	return map[string]any{
		"session_id": m.id, "status": m.status,
		"level": m.level, "captions": captions,
	}
}
