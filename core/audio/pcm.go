package audio

import (
	"encoding/binary"
	"fmt"
)

const (
	pcmOutputRate   = 16000
	maxPCMInputSize = 16 << 20
)

// PCMFormat describes interleaved, little-endian signed PCM16 input.
type PCMFormat struct {
	SampleRate    int
	Channels      int
	BitsPerSample int
}

func (f PCMFormat) validate() error {
	if f.SampleRate < 8000 || f.SampleRate > 384000 {
		return fmt.Errorf("invalid PCM sample rate: %d", f.SampleRate)
	}
	if f.Channels < 1 || f.Channels > 32 {
		return fmt.Errorf("invalid PCM channel count: %d", f.Channels)
	}
	if f.BitsPerSample != 16 {
		return fmt.Errorf("unsupported PCM sample size: %d", f.BitsPerSample)
	}
	return nil
}

// PCMConverter converts interleaved PCM16 input into consecutive 20 ms
// mono 16 kHz PCM16 frames. A converter belongs to one capture stream.
// It retains fractional resampling and incomplete output frames across packets.
type PCMConverter struct {
	format    PCMFormat
	remaining int64
	sum       int64
	frame     [FrameBytes]byte
	used      int
}

func NewPCMConverter(format PCMFormat) (*PCMConverter, error) {
	if err := format.validate(); err != nil {
		return nil, err
	}
	return &PCMConverter{format: format, remaining: int64(format.SampleRate)}, nil
}

// PacketBytes checks the native packet frame count before the caller forms a
// slice from an external buffer. Limit packets to ten seconds and 16 MiB.
func (c *PCMConverter) PacketBytes(frames int) (int, error) {
	if c == nil || c.format.SampleRate == 0 {
		return 0, fmt.Errorf("PCM converter is not initialized")
	}
	bytesPerFrame := c.format.Channels * 2
	if frames < 0 || frames > c.format.SampleRate*10 || frames > maxPCMInputSize/bytesPerFrame {
		return 0, fmt.Errorf("invalid PCM packet frame count: %d", frames)
	}
	return frames * bytesPerFrame, nil
}

// Consume validates the entire packet before mutating the resampler state.
// Silent packets advance timing by the reported frame count without reading data.
// push receives a reusable frame buffer; consumers must copy to retain it.
func (c *PCMConverter) Consume(data []byte, frames int, silent bool, push func([]byte)) error {
	size, err := c.PacketBytes(frames)
	if err != nil {
		return err
	}
	if !silent && len(data) != size {
		return fmt.Errorf("PCM packet has %d bytes, want %d", len(data), size)
	}
	for index := 0; index < frames; index++ {
		var mono int64
		if !silent {
			base := index * c.format.Channels * 2
			for channel := 0; channel < c.format.Channels; channel++ {
				mono += int64(int16(binary.LittleEndian.Uint16(data[base+2*channel:])))
			}
			mono /= int64(c.format.Channels)
		}
		span := int64(pcmOutputRate)
		for span > 0 {
			take := min(span, c.remaining)
			c.sum += mono * take
			c.remaining -= take
			span -= take
			if c.remaining == 0 {
				binary.LittleEndian.PutUint16(c.frame[c.used:], uint16(int16(c.sum/int64(c.format.SampleRate))))
				c.used += 2
				if c.used == len(c.frame) {
					push(c.frame[:])
					c.used = 0
				}
				c.remaining = int64(c.format.SampleRate)
				c.sum = 0
			}
		}
	}
	return nil
}
