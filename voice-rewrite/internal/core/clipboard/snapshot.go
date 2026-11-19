package clipboard

import (
	"fmt"
	"time"
)

const (
	// TextReadMax is the largest Unicode clipboard block a selection read accepts.
	// It is not the JSONL frame limit, even when the two numbers match.
	TextReadMax = 8 * 1024 * 1024
	// FormatMaxBytes is one snapshot format. TotalMaxBytes is the whole snapshot.
	FormatMaxBytes = 16 << 20
	TotalMaxBytes  = 48 << 20
	OpenAttempts   = 20
	OpenPause      = 25 * time.Millisecond

	OpenFailed         = "无法打开剪贴板（其他程序可能正在占用）"
	ReadFailed         = "读取剪贴板失败"
	TextLengthRejected = "剪贴板文本长度异常"
	AllocFailed        = "剪贴板内存分配失败"
	LockFailed         = "无法锁定剪贴板内存"
	EmptyFailed        = "无法清空剪贴板"
	WriteFailed        = "无法写入剪贴板"
	RestoreAllocFailed = "剪贴板恢复失败：内存分配失败，原内容尚未更改"
	RestoreLockFailed  = "剪贴板恢复失败：无法锁定内存，原内容尚未更改"
	RestoreEmptyFailed = "剪贴板恢复失败：无法清空临时内容"
)

// TextBlockReadable accepts a UTF-16 block of at least one code unit, up to TextReadMax.
func TextBlockReadable(bytes uintptr) bool {
	return bytes >= 2 && bytes <= TextReadMax
}

// TextTerminated is false when the buffer has no trailing NUL inside its allocation.
func TextTerminated(index, units int) bool {
	return index < units
}

// SnapshotFits refuses an empty block and a snapshot that would pass either byte cap.
func SnapshotFits(size, total uintptr) bool {
	return size != 0 && size <= FormatMaxBytes && total <= TotalMaxBytes && size <= TotalMaxBytes-total
}

// UnsafeFormat is a bitmap, palette, metafile, or owner-display clipboard format.
// Those handles are not copied, because replacing them can drop the user's data.
func UnsafeFormat(id uint32) bool {
	switch id {
	case 2, 3, 9, 14, 0x80, 0x82, 0x83, 0x8e:
		return true
	}
	return false
}

// SnapshotFormatReady stops at the first reason a format cannot be snapshotted.
// Later callbacks are not called.
func SnapshotFormatReady(id uint32, handleOK, sizeOK, lockOK func() bool) error {
	if UnsafeFormat(id) {
		return fmt.Errorf("剪贴板包含图像或系统对象（格式 %d），为避免丢失数据已取消自动操作", id)
	}
	if !handleOK() {
		return fmt.Errorf("剪贴板格式 %d 无法复制，已取消自动操作", id)
	}
	if !sizeOK() {
		return fmt.Errorf("剪贴板格式 %d 不是可安全复制的内存数据，已取消自动操作", id)
	}
	if !lockOK() {
		return fmt.Errorf("无法锁定剪贴板格式 %d，已取消自动操作", id)
	}
	return nil
}

func RestorePartial(failed int) error {
	return fmt.Errorf("剪贴板恢复部分失败：%d 个格式未能写回", failed)
}
