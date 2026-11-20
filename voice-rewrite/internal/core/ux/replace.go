package ux

const (
	// These Windows replacement sentences stay distinct from dictation.
	// The held-key wait here is 3s and the wording names Ctrl/Alt.
	ReplaceExpired          = "任务已过期，禁止覆盖原文"
	ReplaceReleaseKeys      = "请松开 Ctrl/Alt 等快捷键后再试"
	ReplaceWindowSwitched   = "编辑窗口已经切换；结果保留在预览卡片，可手动复制"
	ReplaceClipboardChanged = "执行期间剪贴板被其他程序更改，为避免覆盖用户数据已取消自动写入"
	ReplaceSelectionChanged = "原选区已变化或无法读取；结果保留在预览卡片，不自动覆盖"
	ReplaceBeforePaste      = "粘贴前目标窗口发生变化，已取消自动写入"
	ReplacePasteStopped     = "目标窗口变化或任务已取消，已停止粘贴"
	ReplaceWindowAfterPaste = "粘贴后窗口变化，无法验证结果；可使用预览卡片"
	ReplacePasteRefused     = "原文字仍被选中，目标应用可能拒绝粘贴；结果可从预览卡片复制"
	ReplaceRestoreFailed    = "已尝试粘贴，但剪贴板恢复失败"
)

