package meeting

import (
	"strings"
	"time"
	"unicode/utf16"
)

// A translation of a short ASR fragment cannot legitimately contain whole
// paragraphs from preceding turns. Reject such output instead of showing an
// invented recap as the current subtitle.
func TranslationFitsSource(source, translated string) bool {
	source = strings.TrimSpace(source)
	translated = strings.TrimSpace(translated)
	if source == "" || translated == "" {
		return false
	}
	return len([]rune(translated)) <= max(48, len([]rune(source))*2)
}

// Conversation is useful for pronouns and references, not general knowledge.
// Strip it for standalone requests so a project-specific profile cannot
// monopolize factual answers or casual speaking tasks.
func UsesConversationContext(spoken string) bool {
	lower := strings.ToLower(spoken)
	for _, phrase := range []string{
		"刚才", "上面", "之前说", "刚提到", "他说", "她说", "他们说",
		"这两个方案", "这几个方案", "这个方案", "这些方案", "对方", "这个问题",
		"这个观点", "这点", "这件事", "这个说法", "我想补充", "我要回应", "我想插一句",
		"those options", "these options", "that proposal", "earlier in the meeting",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func ExpressionHeading(status string, hasResult bool) string {
	if strings.TrimSpace(status) == "" {
		return "我的英文表达"
	}
	if hasResult {
		return "我的英文表达 · 上次结果仍可复制"
	}
	return "我的英文表达 · 正在准备"
}

func ExpressionDetail(status, source string) string {
	status, source = strings.TrimSpace(status), strings.TrimSpace(source)
	switch {
	case status != "" && source != "":
		return status + "\r\n已识别 · " + source
	case status != "":
		return status
	case source != "":
		return "已识别 · " + source
	default:
		return "语音原话会显示在这里"
	}
}

// TranscriptAtBottom reports whether an edit control is within two lines of its end.
func TranscriptAtBottom(maxPos, pos, page int32) bool {
	remaining := maxPos - pos - max(int32(0), page-1)
	return remaining <= 2
}

func TranscriptStep(currentID, previousID uint64, historyCount, previousHistory int, live bool, last, now time.Time) (newTurn, paint bool) {
	newTurn = currentID != previousID || historyCount > previousHistory
	paint = TextDue(last, now, newTurn || !live)
	return newTurn, paint
}

func ExpressionStep(complete bool, english, previous string, captionsOnly, expanded bool, last, now time.Time) (copy string, captions, expand, fresh, paint bool) {
	fresh = complete && english != "" && english != previous
	captions, expand = ExpressionFocus(complete, fresh, captionsOnly, expanded)
	if complete {
		copy = english
	}
	paint = TextDue(last, now, complete)
	return copy, captions, expand, fresh, paint
}

func ExpressionFocus(completed, newResult, captionsOnly, expanded bool) (bool, bool) {
	if !completed || !newResult {
		return captionsOnly, expanded
	}
	return false, true
}

func VoiceMode(dictation bool) string {
	if dictation {
		return "express"
	}
	return "compose"
}

// RefreshReading reports whether captions, replies, and the expression update.
// A paused meeting still updates the status line and the button faces.
func RefreshReading(paused bool) bool {
	return !paused
}

// ShownUnread clears the unread count when the reader is on the latest turn.
// That clear still happens when the history text itself did not change.
func ShownUnread(following bool, unread int) int {
	if following {
		return 0
	}
	return unread
}

// ExpressionCopyEnabled is the expression copy button.
// Reply copy also requires a finished reply. A stored expression is enough here.
func ExpressionCopyEnabled(expression string) bool {
	return expression != ""
}

// Only new completed history turns count as unread while a reader is away.
func UnreadHistory(unread, priorCount, nextCount int, following bool) int {
	if following {
		return 0
	}
	return unread + max(0, nextCount-priorCount)
}

func FollowLatest(current, moved, atBottom bool) bool {
	if moved {
		return atBottom
	}
	return current
}

// TextPatch describes the smallest end-anchored change to a Windows
// multiline edit control. Its indexes use UTF-16 code units, not Go bytes.
type TextPatch struct {
	Start, End int
	Insert     string
}

func TextChange(old, next string) TextPatch {
	a, b := []rune(old), []rune(next)
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return TextPatch{
		Start:  len(utf16.Encode(a[:n])),
		End:    len(utf16.Encode(a)),
		Insert: string(b[n:]),
	}
}

// Shift saved selections past the changed tail, without pointing into deleted
// text or disrupting an existing selection in the stable transcript prefix.
func MapSelection(pos uint32, patch TextPatch) uint32 {
	at := int(pos)
	if at <= patch.Start {
		return pos
	}
	if at < patch.End {
		return uint32(patch.Start)
	}
	inserted := len(utf16.Encode([]rune(patch.Insert)))
	return uint32(at + inserted - (patch.End - patch.Start))
}
