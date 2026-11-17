package clipboard

import (
	"testing"
	"time"
)

func TestRestoreReady(t *testing.T) {
	tests := []struct {
		name string
		elapsed, sinceLastRead time.Duration
		hasRead, ready bool
	}{
		{"no receipt", time.Second, 0, false, false},
		{"early clipboard manager read", 250 * time.Millisecond, 200 * time.Millisecond, true, false},
		{"minimum dwell not elapsed", 699 * time.Millisecond, 600 * time.Millisecond, true, false},
		{"read still active", time.Second, 50 * time.Millisecond, true, false},
		{"read quiet", time.Second, 250 * time.Millisecond, true, true},
		{"no receipt timeout", 3 * time.Second, 0, false, true},
		{"read within timeout", 3 * time.Second, 5 * time.Millisecond, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RestoreReady(PasteReceipt{Elapsed: tt.elapsed,
				SinceLastRead: tt.sinceLastRead, HasRead: tt.hasRead})
			if got != tt.ready {
				t.Fatalf("restore=%v want=%v", got, tt.ready)
			}
		})
	}
}
