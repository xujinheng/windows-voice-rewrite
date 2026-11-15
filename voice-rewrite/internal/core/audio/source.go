package audio

import "context"

// FrameSource is the only audio contract the portable meeting pipeline needs.
// A platform adapter produces consecutive, 20 ms frames of 16 kHz mono
// signed PCM16. Stream returns when the selected source stops or ctx ends.
// Callbacks execute serially; the consumer must copy frames it retains.
type FrameSource interface {
	Stream(ctx context.Context, started func(), push func([]byte)) error
}
