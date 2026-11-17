package meeting

import (
	"fmt"
	"strings"
)

const CaptionFocusTitle = "专注字幕"
const CaptionFullTitle = "完整视图"
const ExpressionExpandTitle = "放大英文"
const ExpressionCollapseTitle = "收起英文"

const (
	SpeakerLabel      = "当前发言 · 中文优先"
	HistoryTitle      = "之前的对话"
	ReplyLabel        = "回复建议"
	WaitingHint       = "等待发言"
	RefineCue         = "改写要求，例如：更简短、先说明风险"
	ProfileTitle      = "会议资料 · 保存后立即用于本次会议和下次启动"
	ProfileBackground = "背景：角色、项目、目标、已知限制"
	ProfileTerms      = "术语：人名、缩写、产品名和中英对照（每行一条）"
	ProfileMode       = "会议识别方式"
	ProfileSaveFailed = "保存失败: "
	ProfileModeFailed = "资料已保存；识别方式保存失败: "
	ProfileOpenFailed = "打开会议资料失败: "
	CopyFailed        = "复制失败: "
)

// ProfileSaveStep is the next profile-panel action.
// A text failure does not try the recognition mode. A mode failure leaves the panel open.
type ProfileSaveStep struct {
	TryMode      bool
	Close        bool
	StatusPrefix string
}

func AfterProfileText(failed bool) ProfileSaveStep {
	if failed {
		return ProfileSaveStep{StatusPrefix: ProfileSaveFailed}
	}
	return ProfileSaveStep{TryMode: true}
}

func AfterProfileMode(failed bool) ProfileSaveStep {
	if failed {
		return ProfileSaveStep{StatusPrefix: ProfileModeFailed}
	}
	return ProfileSaveStep{Close: true}
}

// ButtonView is the meeting state that changes a button's label.
// SelectedReply < 0 leaves every reply button unmarked.
type ButtonView struct {
	Paused             bool
	CapturePaused      bool
	CaptionsOnly       bool
	ExpressionExpanded bool
	Pending            bool
	SelectedReply      int
}

func ButtonFace(id int, view ButtonView) string {
	switch id {
	case ButtonPause:
		if view.Paused {
			return "恢复画面"
		}
		return "冻结画面"
	case ButtonCaptions:
		if view.CaptionsOnly {
			return CaptionFullTitle
		}
		return CaptionFocusTitle
	case ButtonSmallerText:
		return "A−"
	case ButtonLargerText:
		return "A+"
	case ButtonCopyReply:
		return "复制回复"
	case ButtonCopyExpression:
		return "复制英文"
	case ButtonRequestReplies:
		return "生成建议"
	case ButtonToggleCapture:
		if view.CapturePaused {
			return "恢复采集"
		}
		return "暂停采集"
	case ButtonOpenProfile:
		return "会议资料"
	case ButtonReplyFirst, ButtonReplySecond, ButtonReplyThird:
		names := [3]string{"直接回应", "澄清一下", "争取时间"}
		index := id - ButtonReplyFirst
		title := fmt.Sprintf("%d %s", index+1, names[index])
		if index == view.SelectedReply {
			return "● " + title
		}
		return title
	case ButtonAcceptReplies:
		if view.Pending {
			return "查看新建议"
		}
		return "无新建议"
	case ButtonRefine:
		return "改写回复"
	case ButtonScrollLatest:
		return "回到最新"
	case ButtonExpression:
		if view.ExpressionExpanded {
			return ExpressionCollapseTitle
		}
		return ExpressionExpandTitle
	case ButtonSaveProfile:
		return "保存并应用"
	case ButtonCloseProfile:
		return "取消"
	default:
		return ""
	}
}

// ReadingBody is a caption, translation, reply, expression, instruction, or profile field.
// The live caption is not one of these fields.
func ReadingBody(captions, english, chinese, responses, expression, source, instruction, background, terms bool) bool {
	return captions || english || chinese || responses || expression || source || instruction || background || terms
}

// ReadingPaint uses the primary ink for the live caption and the surface for every reading control.
// The live caption stays on the surface even when the caller does not repeat that.
func ReadingPaint(live, reading bool) (primaryText, surface bool) {
	if live {
		return true, true
	}
	return false, reading
}

func LiveCaption(hint string) string {
	return "● " + hint
}

func StatusText(sourceName, status, stage string, dropped int, capturePaused, frozen bool) string {
	source := strings.Join(strings.Fields(sourceName), " ")
	if source == "" {
		source = "等待音频源"
	}
	if runes := []rune(source); len(runes) > 34 {
		source = string(runes[:33]) + "…"
	}
	switch {
	case capturePaused:
		status = "采集已暂停 · 不再接收音频"
	case frozen:
		status = "画面已冻结 · 后台继续处理"
	case status == "":
		status = "正在聆听"
	}
	detail := source
	if stage != "" {
		detail += "  ·  " + stage
	}
	if dropped > 0 {
		detail += fmt.Sprintf("  ·  跳过 %d 个过期片段", dropped)
	}
	return status + "\r\n" + detail
}

func HistoryHeading(count, unread int) string {
	label := fmt.Sprintf("%s · %d 段", HistoryTitle, count)
	if unread > 0 {
		label += fmt.Sprintf(" · %d 条新内容", unread)
	}
	return label
}

func ReplyBody(english, chinese, tip string, complete bool) string {
	text := english + "\r\n" + chinese
	if english != "" && !complete {
		text += "\r\n正在生成…（完成后可复制或改写）"
	}
	if english == "" {
		text = "听到完整发言后生成建议；新建议可点击“查看新建议”后阅读。"
	}
	if tip != "" {
		text += "\r\n提示 · " + tip
	}
	return text
}

func ExpressionShown(english string) string {
	if english == "" {
		return "听写键：直译自己的发言；改写键：提问、回答、补充观点。"
	}
	return english
}
