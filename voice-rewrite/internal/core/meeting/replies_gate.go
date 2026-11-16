package meeting

import (
	"strings"
	"time"
)

const (
	// ReplyTimeout bounds one reply batch and one refine. It is not the 50s expression timeout.
	ReplyTimeout = 30 * time.Second

	// CapturePausedStatus is the session line. The HUD uses a different paused sentence.
	CapturePausedStatus = "采集已暂停 · 不发送音频"
	CapturePausedStage  = "已暂停"
	CaptureConnecting   = "正在连接音频…"
	StageTranslating    = "正在翻译"
	StageReplies        = "正在准备建议"
	StageExpressing     = "正在整理你的表达"
	RepliesPreparing    = "正在准备回复建议…"
	RepliesFailed       = "建议生成失败 · 保留当前回复"
	RepliesWait         = "请等当前发言结束后再生成建议"
	RefineRunning       = "正在微调所选回复…"
	RefineFailed        = "微调失败，请重试"
)

// InferenceStage picks the metrics label. An express-priority call wins over reply mode,
// so a refine shows the expression stage rather than the reply stage.
func InferenceStage(mode string, expressPriority bool) string {
	stage := StageTranslating
	if mode == "replies" {
		stage = StageReplies
	}
	if expressPriority {
		stage = StageExpressing
	}
	return stage
}

// ReplyRequest stays quiet while capture is paused, and asks the user to wait
// when the latest turn is not finished.
func ReplyRequest(capturePaused, hasCompletedCaption bool) (status string, start bool) {
	if capturePaused {
		return "", false
	}
	if !hasCompletedCaption {
		return RepliesWait, false
	}
	return "", true
}

// ReplyPrior drops a finished turn that is already the caption being answered.
func ReplyPrior(prior []string, latest string) []string {
	out := append([]string(nil), prior...)
	if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == strings.TrimSpace(latest) {
		out = out[:len(out)-1]
	}
	return out
}

// RefineMayRun requires English text and a live capture. The instruction was already accepted.
func RefineMayRun(selected string, capturePaused bool) bool {
	return selected != "" && !capturePaused
}
