// Package ux maps known provider and editor failures to fixed recovery text.
// The catalog never echoes request bodies, selected text, or API keys.
package ux

import "strings"

const (
	unsupportedAudio = "语音接口不支持上传的音频格式（HTTP 415）。请确认 ASR 服务提供 /v1/audio/transcriptions，而不只是 /v1/chat/completions。"
	authFailed       = "API 验证失败。检查服务地址、模型 ID 和 API Key。"
	unreachable      = "无法连接模型服务。请启动 ASR / LLM 服务并检查端口，然后在设置中测试连接。"
	timedOut         = "模型服务响应超时。检查模型是否仍在加载、GPU 内存与网络连接。"
	selectionMoved   = "原选区或编辑窗口发生变化，自动写入已取消。请重新选中文字再试。"
	// Fixed sentences produced by this program. They contain no provider body,
	// selected text, or API key, so both hosts may show them unchanged.
	NoSpeech         = "ASR 没有识别到语音，请检查麦克风输入设备"
	EmptyRecording   = "录音为空或 PCM 长度无效"
	RecordingTooLong = "录音超出时长限制"
	BadASRResponse   = "ASR 响应不是有效的 JSON {text:...} 或 text/plain"
	EmptySelection   = "选中文字为空"
	EmptyInstruction = "语音指令为空"
	EmptyChoices     = "文本模型返回空 choices"
	EmptyModelText   = "文本模型返回空内容；请换用非 thinking 模式的 Instruct 文本模型"
	DictationEmpty   = "听写结果为空"
)

// Classify returns a fixed recovery string for a known failure.
// Unknown errors return ok=false so a host can keep its own constant message
// instead of forwarding provider text that may contain private data.
func Classify(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	message := err.Error()
	if recovered, ok := fixedRecovery(message); ok {
		return recovered, true
	}
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "http 415"):
		return unsupportedAudio, true
	case strings.Contains(lower, "401") || strings.Contains(lower, "403"):
		return authFailed, true
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "connectex"):
		return unreachable, true
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return timedOut, true
	case strings.Contains(message, "选区") || strings.Contains(message, "窗口已"):
		return selectionMoved, true
	default:
		return "", false
	}
}

func fixedRecovery(message string) (string, bool) {
	switch message {
	case NoSpeech, EmptyRecording, RecordingTooLong, BadASRResponse,
		EmptySelection, EmptyInstruction, EmptyChoices, EmptyModelText,
		DictationEmpty, DictationTooShort, RewriteTooShort:
		return message, true
	}
	rest, ok := strings.CutPrefix(message, "选中文字超过 ")
	if !ok {
		return "", false
	}
	count, ok := strings.CutSuffix(rest, " 字限制")
	if !ok || count == "" {
		return "", false
	}
	for _, r := range count {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return message, true
}
