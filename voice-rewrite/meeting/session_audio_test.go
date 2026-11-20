package meeting

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/audio"
)

type framesForMeetingTest struct {
	frames [][]byte
}

func (s framesForMeetingTest) Stream(ctx context.Context, started func(), push func([]byte)) error {
	started()
	for _, frame := range s.frames {
		if err := ctx.Err(); err != nil {
			return err
		}
		push(frame)
	}
	return nil
}

func TestPreviewIntervalFollowsLatencyBounds(t *testing.T) {
	for _, tc := range []struct {
		elapsed, want time.Duration
	}{
		{100 * time.Millisecond, 800 * time.Millisecond},
		{time.Second, 1250 * time.Millisecond},
		{5 * time.Second, 2400 * time.Millisecond},
	} {
		if got := PreviewInterval(tc.elapsed); got != tc.want {
			t.Errorf("PreviewInterval(%v) = %v, want %v", tc.elapsed, got, tc.want)
		}
	}
}

func TestStreamAudioEmitsSnapshotsAndFinalTurn(t *testing.T) {
	voice := make([]byte, audio.FrameBytes)
	for i := 0; i < len(voice); i += 2 {
		binary.LittleEndian.PutUint16(voice[i:], 2500)
	}
	silence := make([]byte, audio.FrameBytes)
	var frames [][]byte
	for i := 0; i < 85; i++ {
		frames = append(frames, voice)
	}
	for i := 0; i < 40; i++ {
		frames = append(frames, silence)
	}
	var started, utterances, previews, finals, metered int
	var finalPCM []byte
	err := StreamAudio(context.Background(), framesForMeetingTest{frames: frames}, func() time.Duration {
		return 100 * time.Millisecond
	}, func() { started++ }, func(event AudioEvent) {
		if event.UtteranceStarted {
			utterances++
		}
		if event.Meter {
			metered++
		}
		if event.PCM != nil {
			if event.Final {
				finals++
				finalPCM = event.PCM
			} else {
				previews++
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if started != 1 || utterances != 1 || previews < 1 || finals != 1 || metered < 1 {
		t.Fatalf("ready=%d utterances=%d previews=%d finals=%d metered=%d", started, utterances, previews, finals, metered)
	}
	if len(finalPCM) < 85*audio.FrameBytes {
		t.Fatalf("final PCM unexpectedly shortened: %d", len(finalPCM))
	}
}

func TestStreamAudioIgnoresFramesAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	silence := make([]byte, audio.FrameBytes)
	source := framesForMeetingTest{frames: [][]byte{silence, silence}}
	var count int
	err := StreamAudio(ctx, source, nil, func() { cancel() }, func(AudioEvent) { count++ })
	if err != context.Canceled || count != 0 {
		t.Fatalf("canceled stream returned %v and emitted %d events", err, count)
	}
}
