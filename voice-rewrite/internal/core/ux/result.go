package ux

import "strings"

const (
	// DictationHistoryTitle is the session-history label for a transcript.
	DictationHistoryTitle = "听写结果"
	// DictationNotInserted is the result card when dictation had no safe target.
	DictationNotInserted = "听写结果 · 未自动插入"
	// DictationCopyManually is the result card when insertion was attempted and failed.
	DictationCopyManually = "听写结果 · 请手动复制"
	// DictationWriteUnconfirmed is for an attempted native write whose result cannot be verified.
	DictationWriteUnconfirmed = "听写结果 · 请确认是否已输入"
	// CopyAction is the result-card button that puts the text on the clipboard.
	CopyAction = "复制结果"
	// ApplyAction is the Windows result-card button that writes the text back.
	ApplyAction = "应用到原文"
	// CloseGlyph is the result-card close mark painted by both hosts.
	CloseGlyph = "×"
	// CopySucceeded is the card status after the result is on the clipboard.
	CopySucceeded = "已复制结果到剪贴板"
	// CopyFailed is the card status when the clipboard cannot be set.
	CopyFailed = "复制失败，请尝试选中正文复制"
	// ApplyBusy is the card status when another voice task still owns the editor.
	ApplyBusy = "任务尚未结束，请稍后点击应用"
	// ApplyChecking is the card status while the original selection is verified.
	ApplyChecking = "正在核验原选区并应用…"
	// ApplyUnsafe is the card status when the selection cannot be replaced.
	ApplyUnsafe = "无法安全替换，请使用复制结果"
	// SaveWhileBusy is the settings message when a voice task is still running.
	SaveWhileBusy = "请先完成当前录音／改写，再保存"
	// WindowsSaveRestart is shown after a shortcut or recording device change.
	// macOS applies shortcuts immediately and keeps its own sentence.
	WindowsSaveRestart = "设置已保存。快捷键和录音设备将在重启 VoiceRewrite 后生效。"
	// WindowsSaveImmediate is shown when only endpoints or models changed.
	WindowsSaveImmediate = "✓ 已保存，API 地址和模型立即生效"
	// SaveFailedPrefix is shown before the error when settings cannot be written.
	SaveFailedPrefix = "保存失败："
	// OpenAdvancedFailedPrefix is shown before the error when the config file cannot be opened.
	OpenAdvancedFailedPrefix = "打开高级配置失败："
	// Tray and pause sentences. Hosts paint icons and own the menu window.
	VoiceRecording    = "正在录音 · 按键松开后提交"
	VoiceTranscribing = "正在识别语音"
	VoiceRewriting    = "正在改写文字"
	VoiceSuccess      = "处理完成"
	VoiceFailed       = "操作失败 · 查看错误提示"
	VoicePaused       = "已暂停 · 鼠标侧键恢复正常功能"
	VoiceIdle         = "待机 · 使用配置的听写和改写快捷键"
	PauseVoice        = "暂停语音操作（保留鼠标侧键原功能）"
	ResumeVoice       = "恢复语音操作"
	PauseWhileBusy    = "当前任务仍在处理；结束后可暂停语音操作。"
	ViewDiagnosticLog = "查看诊断日志"
	// DictationTooShort is shown when a dictation buffer is under 0.1s.
	DictationTooShort = "语音太短，无法转写"
	// RewriteTooShort is shown when a spoken rewrite buffer is under 0.1s.
	RewriteTooShort = "录音太短（至少约 0.1 秒）；请再试一次"
	// SpokenCaptureInvalid is shown when spoken audio is short or not whole PCM16 samples.
	SpokenCaptureInvalid = "录音太短（至少约 0.1 秒）或 PCM 格式无效"
	// Password refusals. Hosts detect the secure field; these sentences stay fixed.
	PasswordDictation = "密码输入框不接受自动听写，请使用普通输入方式"
	PasswordSelection = "密码输入框不允许读取选中文字或发送给 AI"
	PasswordRewrite   = "已拒绝在密码输入框中自动改写"
)

// ResultCardTitle is the floating-card heading for a selected-text result.
// Successful insertion does not show a card; these titles are for preview and
// for a result the user still needs to copy.
func ResultCardTitle(intent string) string {
	switch intent {
	case "edit":
		return "改写预览"
	case "summarize":
		return "AI 摘要"
	case "translate":
		return "翻译结果"
	case "ambiguous":
		return "AI 回答"
	default:
		return "AI 解释"
	}
}

// HistoryTitle is the in-memory session label for a model result.
// The tray and dashboard menu show only the category, not this title.
func HistoryTitle(intent string) string {
	intent = strings.TrimSpace(intent)
	if intent == "" {
		return "AI 结果"
	}
	return "AI " + intent
}

// HistoryRestoreTitle is the card heading when the user reopens a session result.
func HistoryRestoreTitle(storedTitle string) string {
	return strings.TrimSpace(storedTitle) + " · 历史结果"
}
