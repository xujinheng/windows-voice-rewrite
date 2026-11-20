package audio

import "math"

// RMSLevel reports a Handy-style recording-indicator level for mono PCM16.
// The scale is 0..1000. Samples are read in pairs so the indicator stays cheap
// on the capture thread.
func RMSLevel(pcm []byte) uint32 {
	samples := len(pcm) / 2
	if samples == 0 {
		return 0
	}
	var energy float64
	for i := 0; i+1 < len(pcm); i += 4 {
		sample := int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8)
		v := float64(sample) / 32768
		energy += v * v
	}
	n := float64((samples + 1) / 2)
	rms := math.Sqrt(energy / n)
	// Gain matches indicator.MicrophoneLevelGain so both hosts meter the same way.
	level := math.Min(1.0, rms*5.0)
	return uint32(level * 1000)
}
