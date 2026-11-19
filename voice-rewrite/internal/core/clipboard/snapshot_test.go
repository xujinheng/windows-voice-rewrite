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
	called := 0
	err := SnapshotFormatReady(2, func() bool { called++; return true }, func() bool { called++; return true }, func() bool { called++; return true })
	if err == nil || called != 0 || err.Error() != "剪贴板包含图像或系统对象（格式 2），为避免丢失数据已取消自动操作" {
		t.Fatal(err, called)
	}
	err = SnapshotFormatReady(13, func() bool { return false }, func() bool { t.Fatal("size"); return true }, func() bool { return true })
	if err == nil || err.Error() != "剪贴板格式 13 无法复制，已取消自动操作" {
		t.Fatal(err)
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
	if TextTerminated(4, 4) || !TextTerminated(3, 4) {
		t.Fatal("nul")
	}
	if OpenAttempts != 20 || OpenPause.Milliseconds() != 25 {
		t.Fatal("open")
	}
	if LockFailed == RestoreLockFailed || AllocFailed == RestoreAllocFailed || EmptyFailed == RestoreEmptyFailed {
		t.Fatal("restore sentences collapsed into the write path")
	}
}
