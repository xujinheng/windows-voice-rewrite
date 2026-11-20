package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func newPCMForTest(t *testing.T, rate, channels int) *PCMConverter {
	t.Helper()
	c, err := NewPCMConverter(PCMFormat{SampleRate: rate, Channels: channels, BitsPerSample: 16})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPCMConverterFormatsAndChannels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rate   int
		values []int16
		want   int16
	}{
		{"stereo-44100", 44100, []int16{3000, 1000}, 2000},
		{"mono-48000", 48000, []int16{-2400}, -2400},
		{"three-channel-32000", 32000, []int16{-3000, 3000, 6000}, 2000},
		{"upsample-8000", 8000, []int16{1500}, 1500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newPCMForTest(t, tc.rate, len(tc.values))
			packet := make([]byte, tc.rate*len(tc.values)*2)
			for i := 0; i < tc.rate; i++ {
				for ch, value := range tc.values {
					binary.LittleEndian.PutUint16(packet[(i*len(tc.values)+ch)*2:], uint16(value))
				}
			}
			var output []byte
			push := func(frame []byte) { output = append(output, frame...) }
			if err := c.Consume(packet[:len(tc.values)*2*13], 13, false, push); err != nil {
				t.Fatal(err)
			}
			if err := c.Consume(packet[len(tc.values)*2*13:], tc.rate-13, false, push); err != nil {
				t.Fatal(err)
			}
			if len(output) != PCMBytesPerSecond {
				t.Fatalf("output bytes=%d, want %d", len(output), PCMBytesPerSecond)
			}
			for i := 0; i < len(output); i += 2 {
				if got := int16(binary.LittleEndian.Uint16(output[i:])); got != tc.want {
					t.Fatalf("sample at %d: %d, want %d", i/2, got, tc.want)
				}
			}
		})
	}
}

func TestPCMConverterPacketSplitting(t *testing.T) {
	const frames = 44100
	packet := make([]byte, frames*4)
	for i := 0; i < frames; i++ {
		v := int16((i*23)%30000 - 15000)
		binary.LittleEndian.PutUint16(packet[i*4:], uint16(v))
		binary.LittleEndian.PutUint16(packet[i*4+2:], uint16(v/2))
	}
	var one, split []byte
	c1, c2 := newPCMForTest(t, frames, 2), newPCMForTest(t, frames, 2)
	if err := c1.Consume(packet, frames, false, func(frame []byte) { one = append(one, frame...) }); err != nil {
		t.Fatal(err)
	}
	for pos := 0; pos < frames; {
		n := min(frames-pos, 1+(pos%83))
		if err := c2.Consume(packet[pos*4:(pos+n)*4], n, false, func(frame []byte) { split = append(split, frame...) }); err != nil {
			t.Fatal(err)
		}
		pos += n
	}
	if !bytes.Equal(one, split) {
		t.Fatal("packet boundaries changed PCM output")
	}
}

func TestPCMConverterSilence(t *testing.T) {
	c := newPCMForTest(t, 44100, 2)
	var output []byte
	if err := c.Consume(nil, 441, true, func(frame []byte) { output = append(output, frame...) }); err != nil {
		t.Fatal(err)
	}
	if err := c.Consume(nil, 441, true, func(frame []byte) { output = append(output, frame...) }); err != nil {
		t.Fatal(err)
	}
	if len(output) != FrameBytes {
		t.Fatalf("got %d bytes for 20 ms of silence", len(output))
	}
	if !bytes.Equal(output, make([]byte, FrameBytes)) {
		t.Fatal("silence produced nonzero output")
	}
}

func TestPCMConverterRejectsInvalidFormatAndPacket(t *testing.T) {
	for _, format := range []PCMFormat{
		{}, {SampleRate: 0, Channels: 2, BitsPerSample: 16},
		{SampleRate: 44100, Channels: 0, BitsPerSample: 16},
		{SampleRate: 44100, Channels: 33, BitsPerSample: 16},
		{SampleRate: 44100, Channels: 2, BitsPerSample: 24},
	} {
		if _, err := NewPCMConverter(format); err == nil {
			t.Fatalf("accepted %+v", format)
		}
	}
	c := newPCMForTest(t, 44100, 2)
	var called int
	push := func([]byte) { called++ }
	for _, tc := range []struct {
		data   []byte
		frames int
	}{
		{nil, -1}, {nil, 1}, {[]byte{1, 2, 3}, 1}, {make([]byte, 8), 1},
		{nil, math.MaxInt}, {nil, 44100 * 11},
	} {
		if err := c.Consume(tc.data, tc.frames, false, push); err == nil {
			t.Fatalf("accepted packet frames=%d bytes=%d", tc.frames, len(tc.data))
		}
	}
	if called != 0 {
		t.Fatal("invalid packet emitted audio")
	}
	if err := c.Consume(make([]byte, 441*4), 441, false, push); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatal("invalid packet changed resampler phase")
	}
	if err := c.Consume(make([]byte, 441*4), 441, false, push); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("valid packets produced %d frames", called)
	}
}
