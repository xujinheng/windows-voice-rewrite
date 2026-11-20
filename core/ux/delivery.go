package ux

const (
	PasteReceiptMinimumMilliseconds = 700

	// DictationReleaseKeys is the Windows wait for a held shortcut. Callers keep their own timeouts.
	DictationReleaseKeys = "请松开快捷键后重试"
	// DictationWindowChanged is the Windows dictation paste cancelled because the target window moved.
	DictationWindowChanged = "写入前窗口已变化，已取消听写"
	// DictationRestoreFailed is the dictation clipboard restore failure. The replace path keeps its own sentence.
	DictationRestoreFailed = "已触发粘贴，但剪贴板恢复失败"
)
