package meeting

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// ExpressionTimeout bounds one spoken meeting expression. It is not an ASR timeout.
	ExpressionTimeout = 50 * time.Second
	// PriorTurns is how many finished utterances stay available to the next expression.
	PriorTurns = 5
	// MaxInstructionRunes limits user-supplied refine and compose requests in both hosts.
	MaxInstructionRunes = 2000

	// ListeningStage is the metrics stage while audio is being heard.
	// ListeningPrompt is the live session line. An empty status still falls back to "正在聆听".
	ListeningStage         = "正在听"
	ListeningPrompt        = "正在聆听 · 按住听写键说中文"
	CaptionRecognizing     = "正在识别"
	ExpressRecognizing     = "正在识别你的中文"
	MeetingSaveUnavailable = "未配置会议设置保存服务"
	UnknownMeetingASRMode  = "无效的 ASR 模式"
	TranslationUnavailable = "翻译暂不可用 · 原文已保留"
	DroppedWork            = "处理较慢 · 已跳过过期任务，正在跟上最新语音"
	CaptureEnded           = "音频采集意外结束"
	ExpressTranslating     = "正在翻译你说的话…"
	ExpressComposing       = "正在理解表达意图…"
	ExpressMicFailed       = "麦克风录音失败"
	ExpressTooShort        = "没有录到足够语音"
	ExpressTooShortDetail  = "请按住快捷键说话后松开"
	ExpressASRFailed       = "语音识别失败"
	ExpressEmpty           = "语音识别结果为空"
	ExpressEmptyDetail     = "请调整麦克风音量后重试"
	ExpressGenerateFailed  = "英文生成失败"
	WorkCanceled           = "处理已取消"
)

// ValidInstruction is the single meeting instruction rule for both desktop hosts
// and the JSON-lines service. The byte-level transport limit is checked separately.
func ValidInstruction(value string) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= MaxInstructionRunes
}

// CancelPresentation drops an unfinished expression and replaces a visible status.
// A finished expression stays. An empty status stays empty.
func CancelPresentation(expressionComplete bool, status string) (clearExpression bool, nextStatus string) {
	if status != "" {
		status = WorkCanceled
	}
	return !expressionComplete, status
}

// ExpressionLabel names the in-progress expression. Compose is the only special mode.
func ExpressionLabel(mode string) string {
	if mode == "compose" {
		return ExpressComposing
	}
	return ExpressTranslating
}

// ExpressCapture stops on a microphone error before a short buffer.
// The short-buffer sentence is not the dictation or rewrite sentence.
func ExpressCapture(err error, tooShort bool) (label, detail string, stop bool) {
	if err != nil {
		return ExpressMicFailed, "", true
	}
	if tooShort {
		return ExpressTooShort, ExpressTooShortDetail, true
	}
	return "", "", false
}

// ExpressTranscript trims a recognition and stops on an error or an empty result.
func ExpressTranscript(err error, text string) (label, detail, points string, stop bool) {
	if err != nil {
		return ExpressASRFailed, "", "", true
	}
	points = strings.TrimSpace(text)
	if points == "" {
		return ExpressEmpty, ExpressEmptyDetail, "", true
	}
	return "", "", points, false
}

// MeetingSourceLess lists Zoom ahead of other apps, then sorts titles case-insensitively.
func MeetingSourceLess(aName, aTitle, bName, bTitle string) bool {
	aZoom := strings.EqualFold(aName, "Zoom.exe")
	bZoom := strings.EqualFold(bName, "Zoom.exe")
	if aZoom != bZoom {
		return aZoom
	}
	return strings.ToLower(aTitle) < strings.ToLower(bTitle)
}

// RememberPrior keeps the newest finished utterances.
func RememberPrior(prior []string, latest string) []string {
	prior = append(prior, latest)
	if len(prior) > PriorTurns {
		prior = prior[len(prior)-PriorTurns:]
	}
	return prior
}
