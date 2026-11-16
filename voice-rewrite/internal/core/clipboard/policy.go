// Package clipboard holds pasteboard decisions that do not touch an OS board.
// Hosts own the native clipboard, sequence numbers, and format snapshots.
package clipboard

import "time"

// MayRestore reports whether a plain-text snapshot may be written back.
// A zero owned sequence is never proof of ownership, and a changed sequence
// means the user or another app has taken the clipboard.
func MayRestore(hasOriginal bool, owned, current uint64) bool {
	return hasOriginal && owned != 0 && current == owned
}

// AttemptTimeout schedules a synthetic copy retry. The common two-attempt
// budget retries an ignored first copy after 450ms and gives the remainder
// to the second attempt, so the combined wait stays the same. Other budgets
// keep the caller's per-attempt duration.
func AttemptTimeout(base time.Duration, attempt, attempts int) time.Duration {
	if attempts == 2 && base >= 600*time.Millisecond && base <= 1500*time.Millisecond {
		if attempt == 0 {
			return 450 * time.Millisecond
		}
		return base*2 - 450*time.Millisecond
	}
	return base
}
