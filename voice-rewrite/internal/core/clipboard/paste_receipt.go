package clipboard

import "time"

// Receipt-based restoration is shared behavior; native hosts own the board,
// event callbacks and the actual timing loop.
const (
    PasteReceiptQuietMilliseconds = 200
    PasteReceiptMaxMilliseconds = 8000
    PasteReceiptPollMilliseconds = 100
    // Legacy Windows paste remains at its proven v0.10 wait until its native
    // delayed-rendering backend provides real clipboard read receipts.
    WindowsPasteWaitMilliseconds = 250
)

type PasteReceipt struct {
    Elapsed time.Duration
    SinceLastRead time.Duration
    HasRead bool
}

// RestoreReady waits for a quiet period after the last real consumer read.
// If no read is seen, keep the transcript available until the bounded
// timeout rather than restoring stale clipboard text before a slow editor reads.
func RestoreReady(state PasteReceipt) bool {
    if state.HasRead && state.SinceLastRead >= PasteReceiptQuietMilliseconds*time.Millisecond {
        return true
    }
    return state.Elapsed >= PasteReceiptMaxMilliseconds*time.Millisecond
}
