package ux

import "strings"

const (
	// DictationHistoryTitle is the session-history label for a transcript.
	DictationHistoryTitle = "听写结果"
	// DictationNotInserted is the result card when dictation had no safe target.
	DictationNotInserted = "听写结果 · 未自动插入"
	// DictationCopyManually is the result card when insertion was attempted and failed.
	DictationCopyManually = "听写结果 · 请手动复制"
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
