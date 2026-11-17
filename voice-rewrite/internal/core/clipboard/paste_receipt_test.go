package clipboard

import (
    "testing"
    "time"
)

func TestRestoreReadyWaitsForActualReadAndQuietPeriod(t *testing.T) {
    tests := []struct {
        name string
        elapsed, last time.Duration
        read, ready bool
    }{
        {"no receipt", time.Second, 0, false, false},
        {"recent read", time.Second, 50*time.Millisecond, true, false},
        {"quiet read", time.Second, 200*time.Millisecond, true, true},
        {"read at beginning ignored until quiet", 300*time.Millisecond, 250*time.Millisecond, true, true},
        {"bound time with no receipt", 8*time.Second, 0, false, true},
        {"bound time with recent receipt", 8*time.Second, 10*time.Millisecond, true, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := RestoreReady(PasteReceipt{
                Elapsed: tt.elapsed, SinceLastRead: tt.last, HasRead: tt.read,
            })
            if got != tt.ready { t.Fatalf("got %v, want %v", got, tt.ready) }
        })
    }
}
