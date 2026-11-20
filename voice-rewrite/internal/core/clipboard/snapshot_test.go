package clipboard

import "testing"

func TestSnapshotStopsBeforeCopyingAnImage(t *testing.T) {
	for _, id := range []uint32{2, 3, 9, 14, 0x80, 0x82, 0x83, 0x8e} {
		if !UnsafeFormat(id) {
			t.Fatal(id)
		}
	}
	if UnsafeFormat(1) || UnsafeFormat(13) {
		t.Fatal("text formats")
	}
	if SnapshotFits(0, 0) || !SnapshotFits(FormatMaxBytes, 0) || SnapshotFits(FormatMaxBytes+1, 0) {
		t.Fatal("size")
	}
	if SnapshotFits(1, TotalMaxBytes) || SnapshotFits(1, ^uintptr(0)) || SnapshotFits(2, ^uintptr(0)) {
		t.Fatal("reject full or overflowing snapshot")
	}
	if !SnapshotFits(1, TotalMaxBytes-1) || SnapshotFits(2, TotalMaxBytes-1) {
		t.Fatal("exact remaining capacity must be respected")
	}
	if TextBlockReadable(1) || !TextBlockReadable(2) || !TextBlockReadable(TextReadMax) || TextBlockReadable(TextReadMax+1) {
		t.Fatal("text block")
	}
	if OpenAttempts != 20 || OpenPause.Milliseconds() != 25 {
		t.Fatal("open")
	}
	if LockFailed == RestoreLockFailed || AllocFailed == RestoreAllocFailed || EmptyFailed == RestoreEmptyFailed {
		t.Fatal("restore sentences collapsed into the write path")
	}
}
