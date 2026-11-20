package meeting

import (
	"context"
	"time"

	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/audio"
)

// AudioEvent describes an input frame and any speech snapshot produced by it.
// PCM is retained by the receiver; FrameSource's temporary frame is never exposed.
type AudioEvent struct {
	Time             time.Time
	Level            int
	Meter            bool
	UtteranceStarted bool
	Final            bool
	PCM              []byte
}

// PreviewInterval spaces provisional ASR work according to recent inference
// latency, while bounding both the first-feedback delay and request frequency.
func PreviewInterval(elapsed time.Duration) time.Duration {
	return max(800*time.Millisecond, min(2400*time.Millisecond, elapsed*5/4))
}

// StreamAudio runs the shared audio-to-meeting-turn pipeline against any OS
// FrameSource. Callbacks execute serially on the source callback goroutine.
// A new invocation starts with fresh voice segmentation and metering state.
func StreamAudio(ctx context.Context, source audio.FrameSource, responseTime func() time.Duration, started func(), emit func(AudioEvent)) error {
	var segment audio.Segmenter
	var lastMeter time.Time
	return source.Stream(ctx, started, func(frame []byte) {
		if ctx.Err() != nil {
			return
		}
		now := time.Now()
		if responseTime != nil {
			segment.SetPreviewInterval(PreviewInterval(responseTime()))
		}
		event := AudioEvent{Time: now, Level: audio.Level(frame)}
		if now.Sub(lastMeter) >= 150*time.Millisecond {
			lastMeter = now
			event.Meter = true
		}
		event.UtteranceStarted, event.PCM = segment.Push(frame)
		event.Final = event.PCM != nil
		if !event.Final {
			event.PCM = segment.Preview()
		}
		emit(event)
	})
}
