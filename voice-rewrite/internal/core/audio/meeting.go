package audio

import (
	"encoding/binary"
	"time"
)

const (
	FrameBytes  = PCMBytesPerSecond / 50 // 20 ms of 16 kHz mono PCM16
	meetingLeadFrames  = 8
	PauseFrames = 32
	meetingMaxFrames   = 550
)

// Segmenter detects quiet gaps in normalized meeting audio. It keeps
// a small look-behind so the start of a syllable survives onset detection.
type Segmenter struct {
	lead                                     [meetingLeadFrames][FrameBytes]byte
	leadNext, leadCount, loud, quiet, voiced int
	segment                                  []byte
	previewFrames, nextPreview               int
}

func (s *Segmenter) Push(frame []byte) (started bool, completed []byte) {
	var sum int64
	for i := 0; i < FrameBytes; i += 2 {
		sample := int64(int16(binary.LittleEndian.Uint16(frame[i:])))
		sum += sample * sample
	}
	hasSound := sum >= 280*280*(FrameBytes/2)
	if s.segment == nil {
		copy(s.lead[s.leadNext][:], frame)
		s.leadNext = (s.leadNext + 1) % len(s.lead)
		s.leadCount = min(s.leadCount+1, len(s.lead))
		if !hasSound {
			s.loud = 0
			return false, nil
		}
		s.loud++
		if s.loud < 3 {
			return false, nil
		}
		s.segment = make([]byte, 0, 2*PCMBytesPerSecond)
		for i := 0; i < s.leadCount; i++ {
			index := (s.leadNext - s.leadCount + i + len(s.lead)) % len(s.lead)
			s.segment = append(s.segment, s.lead[index][:]...)
		}
		s.voiced = s.loud
		return true, nil
	}

	s.segment = append(s.segment, frame...)
	if hasSound {
		s.voiced++
		s.quiet = 0
	} else {
		s.quiet++
	}
	if s.quiet < PauseFrames && len(s.segment) < meetingMaxFrames*FrameBytes {
		return false, nil
	}
	if s.voiced >= 7 {
		trim := max(0, s.quiet-5) * FrameBytes
		completed = s.segment[:len(s.segment)-trim]
	}
	*s = Segmenter{}
	return false, completed
}

// PCMConverter averages the source interval for each output sample.
// Keeping the fractional remainder across packets prevents timing drift.
type PCMConverter struct {
	remaining, sum int32
	frame          [FrameBytes]byte
	used           int
}

func (c *PCMConverter) Consume(stereo []byte, frames int, push func([]byte)) {
	const inputRate = 44100
	const outputRate = 16000
	if c.remaining == 0 {
		c.remaining = inputRate
	}
	for index := 0; index < frames; index++ {
		var mono int32
		if stereo != nil {
			pos := index * 4
			left := int32(int16(binary.LittleEndian.Uint16(stereo[pos:])))
			right := int32(int16(binary.LittleEndian.Uint16(stereo[pos+2:])))
			mono = (left + right) / 2
		}
		span := int32(outputRate)
		if span >= c.remaining {
			c.sum += mono * c.remaining
			span -= c.remaining
			binary.LittleEndian.PutUint16(c.frame[c.used:], uint16(int16(c.sum/inputRate)))
			c.used += 2
			if c.used == len(c.frame) {
				push(c.frame[:])
				c.used = 0
			}
			c.remaining = inputRate
			c.sum = 0
		}
		c.sum += mono * span
		c.remaining -= span
	}
}

// preview copies the unfinished utterance at the current service budget.
func (s *Segmenter) Preview() []byte {
	frames := s.previewFrames
	if frames == 0 {
		frames = 40
	}
	if s.nextPreview == 0 {
		s.nextPreview = frames
	}
	if len(s.segment)/FrameBytes < s.nextPreview {
		return nil
	}
	s.nextPreview = len(s.segment)/FrameBytes + frames
	return append([]byte(nil), s.segment...)
}
func (s *Segmenter) SetPreviewInterval(interval time.Duration) {
	s.previewFrames = max(40, int(interval/(20*time.Millisecond)))
}
func Level(frame []byte) int {
	peak := 0
	for i := 0; i+1 < len(frame); i += 2 {
		sample := int(int16(binary.LittleEndian.Uint16(frame[i:])))
		if sample < 0 {
			sample = -sample
		}
		peak = max(peak, sample)
	}
	return min(100, peak*100/8192)
}
