package audio

import (
	"encoding/binary"
	"testing"
)

func TestMeetingSegmenter(t *testing.T) {
	var seg Segmenter
	voice := make([]byte, FrameBytes)
	for i := 0; i < len(voice); i += 2 {
		binary.LittleEndian.PutUint16(voice[i:], 2500)
	}
	silence := make([]byte, FrameBytes)
	var turns [][]byte
	for _, count := range []int{15, 15} {
		for i := 0; i < count; i++ {
			_, pcm := seg.Push(voice)
			if pcm != nil {
				t.Fatal("speech ended before a pause")
			}
		}
		for i := 0; i < 40; i++ {
			_, pcm := seg.Push(silence)
			if pcm != nil {
				turns = append(turns, pcm)
			}
		}
	}
	if len(turns) != 2 || len(turns[0]) < 15*FrameBytes {
		t.Fatalf("got %d turns, want two short utterances", len(turns))
	}
}

func TestMeetingShortPauseDoesNotSplitSentence(t *testing.T) {
	var seg Segmenter
	voice := make([]byte, FrameBytes)
	for i := 0; i < len(voice); i += 2 {
		binary.LittleEndian.PutUint16(voice[i:], 2500)
	}
	quiet := make([]byte, FrameBytes)
	for i := 0; i < 12; i++ {
		seg.Push(voice)
	}
	for i := 0; i < 25; i++ {
		if _, done := seg.Push(quiet); done != nil {
			t.Fatal("split sentence after a natural half-second pause")
		}
	}
	for i := 0; i < 6; i++ {
		seg.Push(voice)
	}
	for i := 0; i < PauseFrames; i++ {
		if _, done := seg.Push(quiet); done != nil {
			return
		}
	}
	t.Fatal("full pause never finalized the utterance")
}

func TestMeetingPCMConverter(t *testing.T) {
	var c PCMConverter
	stereo := make([]byte, 44100*4)
	for i := 0; i < len(stereo); i += 4 {
		binary.LittleEndian.PutUint16(stereo[i:], 3000)
		binary.LittleEndian.PutUint16(stereo[i+2:], 1000)
	}
	var bytes int
	push := func(frame []byte) {
		bytes += len(frame)
		for i := 0; i < len(frame); i += 2 {
			if sample := binary.LittleEndian.Uint16(frame[i:]); sample != 2000 {
				t.Fatalf("mono sample = %d, want 2000", sample)
			}
		}
	}
	c.Consume(stereo[:124], 31, push)
	c.Consume(stereo[124:], 44100-31, push)
	if bytes != PCMBytesPerSecond {
		t.Fatalf("output = %d bytes, want %d", bytes, PCMBytesPerSecond)
	}
}

func TestMeetingPreviewDuringContinuousSpeech(t *testing.T) {
	var s Segmenter
	frame := make([]byte, FrameBytes)
	for i := 0; i < len(frame); i += 2 {
		binary.LittleEndian.PutUint16(frame[i:], 2000)
	}
	var previews int
	var first []byte
	for i := 0; i < 200; i++ {
		_, final := s.Push(frame)
		if final != nil {
			t.Fatal("continuous speech prematurely finalized")
		}
		if snapshot := s.Preview(); snapshot != nil {
			previews++
			if first == nil {
				first = snapshot
			}
		}
	}
	if previews != 5 {
		t.Fatalf("want five previews every 0.8 seconds, got %d", previews)
	}
	first[0] = 0
	if s.segment[0] == 0 {
		t.Fatal("snapshot shares capture buffer")
	}
}
