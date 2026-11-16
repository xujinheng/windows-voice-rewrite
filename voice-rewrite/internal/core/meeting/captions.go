package meeting

import (
	"strings"
	"unicode"
)

// Caption is a platform-neutral meeting utterance. Its translation version is
// kept until fresh Chinese text arrives for the latest source revision.
type Caption struct {
	ID             uint64
	Source         string
	Chinese        string
	TranslatedFrom string
	Stable         string
	Live           string
	Final          bool
}

// ReviseCaption keeps the most recent readable translation while ASR evolves.
func ReviseCaption(old Caption, id uint64, text string, final bool) Caption {
	if old.ID == id && old.Final {
		return old
	}
	out := Caption{ID: id, Source: text, Final: final}
	if old.ID == id {
		// Keep the last readable translation until the replacement arrives.
		out.Chinese = old.Chinese
		out.TranslatedFrom = old.TranslatedFrom
	}
	if final {
		out.Stable = text
		return out
	}
	if old.ID == id {
		a, b := []rune(old.Source), []rune(text)
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
			out.Stable = string(b[:end])
		}
		if strings.HasPrefix(text, old.Stable) && len([]rune(old.Stable)) > len([]rune(out.Stable)) {
			out.Stable = old.Stable
		}
	}
	out.Live = strings.TrimPrefix(text, out.Stable)
	return out
}

// ReadingPanels keeps current recognition outside scrollable history, so
// interim transcription and translation cannot move older text.
type ReadingPanels struct {
	English      string
	Chinese      string
	History      string
	Hint         string
	CurrentID    uint64
	Live         bool
	HistoryCount int
}

func BuildReadingPanels(captions []Caption) ReadingPanels {
	var result ReadingPanels
	var entries []Caption
	for _, line := range captions {
		if strings.TrimSpace(line.Source) != "" {
			entries = append(entries, line)
		}
	}
	if len(entries) == 0 {
		result.English = "等待对方发言…"
		result.Chinese = "中文释义会显示在这里。"
		result.History = "之前的对话会保留在这里，阅读时不会被新字幕打断。"
		result.Hint = WaitingHint
		return result
	}
	current := entries[len(entries)-1]
	result.CurrentID = current.ID
	result.English = strings.TrimSpace(current.Source)
	result.Chinese = strings.TrimSpace(current.Chinese)
	result.Live = !current.Final
	switch {
	case current.TranslatedFrom != "" && current.TranslatedFrom != current.Source:
		result.Hint = "原文已更新 · 译文待刷新"
	case !current.Final:
		result.Hint = "识别中 · 原文仍可能调整"
	case current.Chinese == "":
		result.Hint = "原文已确认 · 正在生成译文"
	default:
		result.Hint = "已确认 · 可以放心阅读"
	}
	if result.Chinese == "" {
		result.Chinese = "正在准备中文释义…"
	}
	for _, item := range entries[:len(entries)-1] {
		if !item.Final {
			continue
		}
		result.HistoryCount++
		if text := strings.TrimSpace(item.Chinese); text != "" {
			result.History += text + "\r\n"
		}
		result.History += strings.TrimSpace(item.Source) + "\r\n\r\n"
	}
	result.History = strings.TrimSpace(result.History)
	if result.HistoryCount == 0 {
		result.History = "已完成的上一句会出现在这里。"
	}
	return result
}

// Suggestions use the latest finished utterance, never partial recognition.
// The caller explicitly requests them; no audio fragment starts them.
func LatestCompletedCaption(captions []Caption) (Caption, bool) {
	for i := len(captions) - 1; i >= 0; i-- {
		if captions[i].Final && strings.TrimSpace(captions[i].Source) != "" {
			return captions[i], true
		}
	}
	return Caption{}, false
}

// UpsertCaption keeps completed utterances immutable and preserves the last
// readable translation while interim ASR revises the source transcript.
func UpsertCaption(captions []Caption, id uint64, text string, final, confirmed bool) []Caption {
	if strings.TrimSpace(text) == "" {
		return captions
	}
	for i := range captions {
		old := captions[i]
		if old.ID != id {
			continue
		}
		if old.Final {
			return captions
		}
		if confirmed {
			captions[i] = ReviseCaption(old, id, text, final)
		} else {
			old.Source = text
			old.Live = strings.TrimPrefix(text, old.Stable)
			if !strings.HasPrefix(text, old.Stable) {
				old.Stable = ""
				old.Live = text
			}
			captions[i] = old
		}
		return captions
	}
	return append(captions, ReviseCaption(Caption{}, id, text, final))
}

// ApplyCaptionTranslation rejects stale translations if recognition has
// already revised the underlying sentence.
func ApplyCaptionTranslation(captions []Caption, id uint64, source, chinese string) bool {
	for i := range captions {
		if captions[i].ID == id && captions[i].Source == source {
			captions[i].Chinese = chinese
			captions[i].TranslatedFrom = source
			return true
		}
	}
	return false
}

func SetCaptionPrefix(captions []Caption, id uint64, prefix string) {
	for i := range captions {
		c := &captions[i]
		if c.ID == id && !c.Final && strings.HasPrefix(c.Source, prefix) {
			c.Stable = prefix
			c.Live = strings.TrimPrefix(c.Source, prefix)
			return
		}
	}
}

// KeepCompletedCaptions preserves all completed history, dropping unfinished
// recognition results when a capture epoch is invalidated.
func KeepCompletedCaptions(captions []Caption) []Caption {
	kept := captions[:0]
	for _, c := range captions {
		if c.Final {
			kept = append(kept, c)
		}
	}
	return kept
}
