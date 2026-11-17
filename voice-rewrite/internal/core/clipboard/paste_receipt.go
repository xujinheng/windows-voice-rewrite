package clipboard

import "time"

// Values here are the single source for native pasteboard timing.
const (
	PasteReceiptQuietMilliseconds   = 200
	PasteReceiptMinimumMilliseconds = 700
	PasteReceiptMaxMilliseconds     = 3000
	PasteReceiptPollMilliseconds    = 100
	WindowsPasteWaitMilliseconds    = 250
)

type PasteReceipt struct {
	Elapsed       time.Duration
	SinceLastRead time.Duration
	HasRead       bool
}

// An eager clipboard manager may fetch the promised text before the target
// handles Cmd+V. Keep it available for the minimum dwell even with a receipt.
// One promised-data callback is not proof of subsequent consumer reads.
func RestoreReady(state PasteReceipt) bool {
	return state.Elapsed >= PasteReceiptMaxMilliseconds*time.Millisecond ||
		(state.Elapsed >= PasteReceiptMinimumMilliseconds*time.Millisecond &&
			state.HasRead && state.SinceLastRead >= PasteReceiptQuietMilliseconds*time.Millisecond)
}
