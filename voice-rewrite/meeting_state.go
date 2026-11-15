package main

import (
	"strings"
	"unicode"
)

// meetingLine is the native HUD view record; the core model's Line is its
// protocol result and is converted at the session boundary.
type meetingLine struct {
	kind, english, chinese string
	complete               bool
}

type meetingCaption struct {
	id                                            uint64
	source, chinese, translatedFrom, stable, live string
	final                                         bool
}

type meetingMetrics struct {
	Stage                                                        string
	Pending, Dropped                                             int
	ASRFirstMS, ASRDoneMS, TranslationFirstMS, TranslationDoneMS int64
}

type meetingView struct {
	status                                               string
	captions                                             []meetingCaption
	replies                                              [3]meetingLine
	tip, replySource, expressionStatus, expressionSource string
	expression                                           meetingLine
	sourceName, asrMode                                  string
	level                                                int
	capturePaused                                        bool
	metrics                                              meetingMetrics
	pendingReplies                                       [3]meetingLine
	pendingReplySource, pendingTip                       string
	replyPending                                         bool
}

func reviseMeetingCaption(old meetingCaption, id uint64, text string, final bool) meetingCaption {
	if old.id == id && old.final {
		return old
	}
	out := meetingCaption{id: id, source: text, final: final}
	if old.id == id {
		// Keep the last readable translation until the replacement arrives.
		out.chinese = old.chinese
		out.translatedFrom = old.translatedFrom
	}
	if final {
		out.stable = text
		return out
	}
	if old.id == id {
		a, b := []rune(old.source), []rune(text)
		n := 0
		for n < len(a) && n < len(b) && a[n] == b[n] {
			n++
		}
		// Keep the last word (or four Chinese characters) provisional.
		end := n
		for end > 0 && !unicode.IsSpace(b[end-1]) {
			end--
		}
		if end == 0 && n > 4 {
			end = n - 4
		}
		if end > 0 {
			out.stable = string(b[:end])
		}
		if strings.HasPrefix(text, old.stable) && len([]rune(old.stable)) > len([]rune(out.stable)) {
			out.stable = old.stable
		}
	}
	out.live = strings.TrimPrefix(text, out.stable)
	return out
}

// meetingTranscript is one continuous reading surface. An unfinished line stays
// at the bottom of the timeline instead of jumping between two text boxes.
func meetingTranscript(captions []meetingCaption) (string, bool) {
	var entries []string
	listening := false
	for _, line := range captions {
		source := strings.TrimSpace(line.source)
		if source == "" {
			continue
		}
		translation := strings.TrimSpace(line.chinese)
		if line.final {
			if translation != "" {
				entries = append(entries, translation+"\r\n"+source)
			} else {
				entries = append(entries, source)
			}
			continue
		}
		listening = true
		if translation != "" {
			entries = append(entries, translation+"\r\n"+source+"  · 识别中")
		} else {
			entries = append(entries, source+"  · 识别中")
		}
	}
	if len(entries) == 0 {
		return "等待会议音频…", false
	}
	return strings.Join(entries, "\r\n\r\n"), listening
}

// meetingReadingPanels keeps the active utterance outside the scrollable
// history, so recognition and translation updates cannot move older text.
type meetingReadingPanels struct {
	english, chinese, history, hint string
	currentID                       uint64
	live                            bool
	historyCount                    int
}

func buildMeetingReadingPanels(captions []meetingCaption) meetingReadingPanels {
	var result meetingReadingPanels
	var entries []meetingCaption
	for _, line := range captions {
		if strings.TrimSpace(line.source) != "" {
			entries = append(entries, line)
		}
	}
	if len(entries) == 0 {
		result.english = "等待对方发言…"
		result.chinese = "中文释义会显示在这里。"
		result.history = "之前的对话会保留在这里，阅读时不会被新字幕打断。"
		result.hint = "等待发言"
		return result
	}
	current := entries[len(entries)-1]
	result.currentID = current.id
	result.english = strings.TrimSpace(current.source)
	result.chinese = strings.TrimSpace(current.chinese)
	result.live = !current.final
	switch {
	case current.translatedFrom != "" && current.translatedFrom != current.source:
		result.hint = "原文已更新 · 译文待刷新"
	case !current.final:
		result.hint = "识别中 · 原文仍可能调整"
	case current.chinese == "":
		result.hint = "原文已确认 · 正在生成译文"
	default:
		result.hint = "已确认 · 可以放心阅读"
	}
	if result.chinese == "" {
		result.chinese = "正在准备中文释义…"
	}
	for _, item := range entries[:len(entries)-1] {
		if !item.final {
			continue
		}
		result.historyCount++
		if text := strings.TrimSpace(item.chinese); text != "" {
			result.history += text + "\r\n"
		}
		result.history += strings.TrimSpace(item.source) + "\r\n\r\n"
	}
	result.history = strings.TrimSpace(result.history)
	if result.historyCount == 0 {
		result.history = "已完成的上一句会出现在这里。"
	}
	return result
}

// A finished voice request must be visible even if the user is listening in
// captions-only mode. A background update never steals the user's view.
// Keep progress/error details subordinate to the last copyable English.
func meetingRetainEnglish(previous meetingLine) meetingLine {
	if previous.complete && strings.TrimSpace(previous.english) != "" {
		return previous
	}
	return meetingLine{}
}

// Suggestions use the latest finished utterance, never partial recognition.
// The caller explicitly requests them; no audio fragment starts them.
func meetingLastCompletedCaption(captions []meetingCaption) (meetingCaption, bool) {
	for i := len(captions) - 1; i >= 0; i-- {
		if captions[i].final && strings.TrimSpace(captions[i].source) != "" {
			return captions[i], true
		}
	}
	return meetingCaption{}, false
}
