package main

import (
	"testing"

	"voicerewrite/internal/core/clipboard"
)

func TestNativeDeliveryPolicies(t *testing.T) {
	cases := []struct {
		name                                              string
		op                                                int
		valid, released, focus, sensitive, selected, want bool
	}{
		{"read_normal", 0, false, false, true, false, true, true},
		{"read_password", 0, false, false, true, true, true, false},
		{"read_missing_target", 0, false, false, false, false, true, false},
		{"dictation_normal", 1, true, true, true, false, true, true},
		{"dictation_held", 1, true, false, true, false, true, false},
		{"dictation_canceled", 1, false, true, true, false, true, false},
		{"dictation_focus_changed", 1, true, true, false, false, true, false},
		{"dictation_secure", 1, true, true, true, true, true, false},
		{"rewrite_normal", 2, true, true, true, false, true, true},
		{"rewrite_selection_changed", 2, true, true, true, false, false, false},
		{"rewrite_secure", 2, true, true, true, true, true, false},
		{"rewrite_stale", 2, false, true, true, false, true, false},
		{"invalid_mode", 3, true, true, true, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deliveryReady(tc.op, tc.valid, tc.released, tc.focus, tc.sensitive, tc.selected); got != tc.want {
				t.Fatalf("deliveryReady(%+v) = %v", tc, got)
			}
		})
	}
}

func TestMacClipboardBudgetMatchesWindowsPolicy(t *testing.T) {
	for _, tc := range []struct {
		size, total int64
	}{
		{1, 0}, {16 << 20, 0}, {1, 48<<20 - 1},
		{16<<20 + 1, 0}, {1, 48 << 20}, {0, 0},
		{-1, 0}, {1, -1}, {16 << 20, 40 << 20},
	} {
		want := tc.size > 0 && tc.total >= 0 &&
			clipboard.SnapshotFits(uintptr(tc.size), uintptr(tc.total))
		if got := clipboardSnapshotFits(tc.size, tc.total); got != want {
			t.Errorf("clipboardSnapshotFits(%d, %d) = %v, want %v", tc.size, tc.total, got, want)
		}
	}
}
