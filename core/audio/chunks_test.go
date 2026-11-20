package audio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMinCaptureIsOneTenthSecond(t *testing.T) {
	if CaptureSampleRate != 16000 || PCMBytesPerSecond != 32000 || MinCaptureBytes != 3200 {
		t.Fatalf("rate=%d bps=%d min=%d", CaptureSampleRate, PCMBytesPerSecond, MinCaptureBytes)
	}
	if !BelowMinCapture(make([]byte, MinCaptureBytes-1)) || BelowMinCapture(make([]byte, MinCaptureBytes)) {
		t.Fatal("0.1s boundary")
	}
}

func TestLongAudioChunksNeverDropSamples(t *testing.T) {
	pcm := make([]byte, 62*PCMBytesPerSecond)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:i+2], uint16(i/2))
	}
	parts := SplitPCMForASR(pcm)
	if len(parts) < 3 {
		t.Fatalf("expected several chunks; got %d", len(parts))
	}
	if !bytes.Equal(bytes.Join(parts, nil), pcm) {
		t.Fatal("lost or reordered samples")
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > ASRChunkSeconds*PCMBytesPerSecond || len(part)%2 != 0 {
			t.Fatalf("invalid PCM chunk length: %d", len(part))
		}
	}
}

func TestJoinASRChunksChineseAndEnglish(t *testing.T) {
	if got := JoinASRChunks([]string{"你好", "世界。"}); got != "你好世界。" {
		t.Fatal(got)
	}
	if got := JoinASRChunks([]string{"Hello", "world."}); got != "Hello world." {
		t.Fatal(got)
	}
}
