package clipboard

import (
	"testing"
	"time"
)

func TestAttemptTimeoutKeepsBudget(t *testing.T) {
	for _, base := range []time.Duration{600 * time.Millisecond, 750 * time.Millisecond, 1200 * time.Millisecond, 1500 * time.Millisecond} {
		first := AttemptTimeout(base, 0, 2)
		second := AttemptTimeout(base, 1, 2)
		if first != 450*time.Millisecond || second < first || first+second != base*2 {
			t.Fatalf("base=%s: first=%s second=%s", base, first, second)
		}
	}
}

func TestAttemptTimeoutPreservesCustomBudgets(t *testing.T) {
	for _, tc := range []struct {
		base     time.Duration
		attempts int
	}{
		{500 * time.Millisecond, 2}, {1800 * time.Millisecond, 2},
		{750 * time.Millisecond, 1}, {750 * time.Millisecond, 3},
	} {
		for attempt := 0; attempt < tc.attempts; attempt++ {
			if got := AttemptTimeout(tc.base, attempt, tc.attempts); got != tc.base {
				t.Errorf("base=%s attempts=%d attempt=%d got=%s", tc.base, tc.attempts, attempt, got)
			}
		}
	}
}
