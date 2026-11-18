package ux

import (
	"strings"

	"voicerewrite/internal/core/delivery"
)

// TaskHistoryEffect is a decision to retain a complete result in session history.
// Native hosts own storage; Go owns which values deserve an entry.
type TaskHistoryEffect struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

// TaskEffect is a portable post-provider/post-write presentation contract.
// A host may paint phases, save history, or execute one guarded native write;
// it must never infer a different write from the body or receipt.
type TaskEffect struct {
	Phase             int                 `json:"phase,omitempty"`
	Mode              string              `json:"mode,omitempty"`
	RecordingStatus   string              `json:"recording_status,omitempty"`
	InteractionStatus string              `json:"interaction_status,omitempty"`
	ASRProbe          string              `json:"asr_probe,omitempty"`
	LLMProbe          string              `json:"llm_probe,omitempty"`
	Transcription     string              `json:"transcription,omitempty"`
	ProcessedText     string              `json:"processed_text,omitempty"`
	History           []TaskHistoryEffect `json:"history,omitempty"`
	CardTitle         string              `json:"card_title,omitempty"`
	CardText          string              `json:"card_text,omitempty"`
	InsertText        string              `json:"insert_text,omitempty"`
	ReplaceText       string              `json:"replace_text,omitempty"`
	ManualReview      bool                `json:"manual_review,omitempty"`
	CancelTask        bool                `json:"cancel_task,omitempty"`
}

// UI phases correspond to the shared indicator phases, not local Swift policies.
const (
	taskSuccess  = 3
	taskError    = 4
	taskApplying = 6
	taskAnswer   = 7
	taskFallback = 9
)

// DictationTaskEffect is used by the Windows ASR callback and voicebridge.
// It makes the history decision once, before any native delivery receipt.
func DictationTaskEffect(text string, hasTarget bool) TaskEffect {
	switch RouteDictationTranscript(text, hasTarget) {
	case DictationRouteEmpty:
		return TaskEffect{Phase: taskError, Mode: "听写",
			RecordingStatus: DictationEmpty, ASRProbe: "ASR：实际转写失败"}
	case DictationRoutePreview:
		return TaskEffect{Phase: taskFallback, Mode: "听写",
			RecordingStatus: "语音识别已完成", ASRProbe: "ASR：真实录音识别成功",
			Transcription: text, History: []TaskHistoryEffect{{DictationHistoryTitle, text}},
			CardTitle: DictationNotInserted, CardText: text}
	default:
		return TaskEffect{Phase: taskApplying, Mode: "听写",
			RecordingStatus: "语音识别已完成", ASRProbe: "ASR：真实录音识别成功",
			Transcription: text, History: []TaskHistoryEffect{{DictationHistoryTitle, text}},
			InsertText: text}
	}
}

// SelectedTaskEffect accepts only a route computed by Go textedit.RouteTask.
// Typed replace uses a review card; spoken replace authorizes one native write.
func SelectedTaskEffect(intent, body, instruction, route string, spoken bool) TaskEffect {
	if strings.TrimSpace(body) == "" || (route != "replace" && route != "preview") {
		return TaskEffect{Phase: taskError, Mode: "改写", CancelTask: true,
			InteractionStatus: "模型处理失败：无有效结果或路由",
			LLMProbe:          "LLM：实际任务失败"}
	}
	if spoken && strings.TrimSpace(instruction) == "" {
		return TaskEffect{Phase: taskError, Mode: "改写", CancelTask: true,
			InteractionStatus: "语音指令响应缺失；未写入原选区",
			LLMProbe:          "LLM：实际任务失败"}
	}
	effect := TaskEffect{Mode: "改写", ProcessedText: body,
		LLMProbe:  "LLM：实际文本任务成功",
		History:   []TaskHistoryEffect{{HistoryTitle(intent), body}},
		CardTitle: ResultCardTitle(intent)}
	if spoken {
		effect.RecordingStatus = "语音修改已处理"
		effect.ASRProbe = "ASR：语音指令识别成功"
		effect.Transcription = instruction
		effect.History = append([]TaskHistoryEffect{{DictationHistoryTitle, instruction}}, effect.History...)
	}
	if route == "preview" {
		effect.Phase = taskAnswer
		effect.InteractionStatus = "仅预览结果；未获模型路由修改授权"
		effect.CardText = body
		effect.CancelTask = true
		return effect
	}
	if !spoken {
		effect.Phase = taskAnswer
		effect.CardText = body
		effect.ManualReview = true
		return effect
	}
	effect.Phase = taskApplying
	effect.ReplaceText = body
	return effect
}

// TaskWriteEffect is the common receipt router for either native host.
// sent_unverified is *not* confirmed success; neither it nor uncertain
// can authorize another automatic write. A fallback card is copy-only.
func TaskWriteEffect(kind delivery.Kind, state delivery.State, text, cardTitle, nativeStatus string) TaskEffect {
	mode := "听写"
	if kind == delivery.ReplaceSelection {
		mode = "改写"
	}
	effect := TaskEffect{Mode: mode}
	switch state {
	case delivery.Confirmed:
		effect.Phase = taskSuccess
		if kind == delivery.Dictation {
			effect.InteractionStatus = "已发送输入指令；请以目标输入框实际内容为准"
		} else {
			effect.InteractionStatus = "原选区验证通过并已替换"
		}
	case delivery.SentUnverified:
		effect.Phase = taskAnswer
		effect.InteractionStatus = "已向目标发送文字，但无法确认是否生效；请先检查输入框，避免重复粘贴"
	case delivery.Blocked, delivery.Uncertain:
		effect.Phase = taskFallback
		effect.InteractionStatus = nativeStatus
		effect.CardTitle = cardTitle
		effect.CardText = text
	default:
		// Unknown wire receipts must fail closed, never claim success.
		effect.Phase = taskFallback
		effect.InteractionStatus = "写入回执无效；请核对原输入框"
		effect.CardTitle = cardTitle
		effect.CardText = text
	}
	return effect
}
