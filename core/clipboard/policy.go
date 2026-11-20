// Package clipboard holds pasteboard decisions that do not touch an OS board.
// Hosts own the native clipboard, sequence numbers, and format snapshots.
package clipboard

import "time"

const (
	// CopySettle lets a mouse key-up finish before Ctrl+C. CopyPoll is the
	// separate wait between clipboard sequence checks. Both are 35ms.
	CopySettle       = 35 * time.Millisecond
	CopyPoll         = 35 * time.Millisecond
	CopyModifierWait = 900 * time.Millisecond

	// A native Windows paste must allow its target to consume the clipboard.
	WindowsPasteWaitMilliseconds = 250

	CopyModifiersHeld = "Ctrl/Alt/Shift 仍被按住，已停止模拟复制；请松开修饰键后再试"
	CopyWindowBefore  = "编辑窗口已切换，已停止自动复制，避免读取其他窗口的文字"
	CopyWindowDuring  = "复制期间编辑窗口发生切换，已停止自动复制"
	CopyUnchanged     = "Ctrl+C 已发送，但剪贴板没有更新（目标编辑器可能未处理快捷键或复制响应较慢）"
	CopyEmpty         = "剪贴板已更新，但没有可读取的选中文字（请确认已选中文字，且目标窗口允许复制）"
	CaptureNoWindow   = "找不到当前编辑窗口"
)

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
