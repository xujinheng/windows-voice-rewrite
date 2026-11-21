package meeting

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMeetingStableTail(t *testing.T) {
	var c Caption
	c = ReviseCaption(c, 1, "We should check the cache", false)
	if c.Stable != "" || c.Live == "" {
		t.Fatal(c)
	}
	c = ReviseCaption(c, 1, "We should check the cache first", false)
	if c.Stable != "We should check the " || c.Live != "cache first" {
		t.Fatalf("%#v", c)
	}
	c = ReviseCaption(c, 1, "We should check the cache first.", true)
	final := c
	c = ReviseCaption(c, 1, "stale preview", false)
	if c != final || !c.Final || c.Live != "" {
		t.Fatal(c)
	}
}

func TestMeetingReadingPanelsKeepActiveTurnOutOfHistory(t *testing.T) {
	captions := []Caption{
		{ID: 1, Source: "We can start today.", Chinese: "今天可以开始。", Final: true},
		{ID: 2, Source: "Do we have a timeline?", Chinese: "有时间计划吗？", Final: true},
		{ID: 3, Source: "The schedule", Final: false},
	}
	panels := BuildReadingPanels(captions)
	if panels.English != "The schedule" || panels.Chinese != "正在准备中文释义…" || !panels.Live {
		t.Fatalf("current provisional reading area = %#v", panels)
	}
	if panels.HistoryCount != 2 || strings.Contains(panels.History, "The schedule") ||
		!strings.Contains(panels.History, "今天可以开始。") || !strings.Contains(panels.History, "Do we have a timeline?") {
		t.Fatalf("history must contain completed older turns only: %#v", panels)
	}
	captions[2].Source = "The schedule is next week."
	captions[2].Chinese = "计划安排在下周。"
	changed := BuildReadingPanels(captions)
	if changed.History != panels.History || changed.HistoryCount != panels.HistoryCount {
		t.Fatal("active translation changed scrollable history")
	}
	if changed.Chinese != "计划安排在下周。" {
		t.Fatalf("translation wasn't updated in fixed pane: %#v", changed)
	}
	captions[2].Final = true
	last := BuildReadingPanels(captions)
	if last.Live || last.English != changed.English || last.History != panels.History {
		t.Fatalf("finalizing current turn moved the reader: %#v", last)
	}
	captions = append(captions, Caption{ID: 4, Source: "What is the deadline?", Final: true})
	next := BuildReadingPanels(captions)
	if next.HistoryCount != 3 || !strings.Contains(next.History, "The schedule is next week.") ||
		strings.Contains(next.History, "What is the deadline?") {
		t.Fatalf("historical turn assignment: %#v", next)
	}
	if next.Hint != "原文已确认 · 正在生成译文" {
		t.Fatalf("caption state must not guess conversational intent: %#v", next)
	}
}

// Empty captions and a zero-ID demo entry must not alter the reading order.
// Only completed turns before the most recent readable caption enter history.
func TestMeetingReadingPanelsSkipEmptyAndUnfinishedHistory(t *testing.T) {
	captions := []Caption{
		{Source: "  first  ", Chinese: "  第一  ", Final: true},
		{Source: "  ", Final: true},
		{Source: "unfinished", Final: false},
		{Source: "last", Final: true},
	}
	view := BuildReadingPanels(captions)
	if view.English != "last" || view.HistoryCount != 1 || view.History != "第一\r\nfirst" {
		t.Fatalf("unexpected history or active turn: %#v", view)
	}
}

func TestMeetingReadingPanelsEmptyOrLongSession(t *testing.T) {
	if got := BuildReadingPanels(nil); got.HistoryCount != 0 || got.English == "" || got.Chinese == "" {
		t.Fatalf("empty layout did not provide placeholders: %#v", got)
	}
	var captions []Caption
	for i := 0; i < 60; i++ {
		captions = append(captions, Caption{Source: "A recorded turn", Final: true})
	}
	got := BuildReadingPanels(captions)
	if got.HistoryCount != 59 || strings.Count(got.History, "A recorded turn") != 59 {
		t.Fatalf("long-session history discarded: %#v", got)
	}
}

func TestMeetingTranslationVersionIsVisibleToReader(t *testing.T) {
	translated := Caption{ID: 1, Source: "I can start Monday", Chinese: "我周一可以开始", TranslatedFrom: "I can start Monday", Final: true}
	ready := BuildReadingPanels([]Caption{translated})
	if ready.Hint != "已确认 · 可以放心阅读" {
		t.Fatalf("completed translation incorrectly marked pending: %q", ready.Hint)
	}
	changed := ReviseCaption(Caption{ID: 1, Source: "I can start Monday", Chinese: "我周一可以开始", TranslatedFrom: "I can start Monday"}, 1, "I can start Tuesday", true)
	if changed.Chinese != translated.Chinese || changed.TranslatedFrom != translated.TranslatedFrom {
		t.Fatalf("changing ASR erased the last readable Chinese version: %#v", changed)
	}
	stale := BuildReadingPanels([]Caption{changed})
	if stale.Hint != "原文已更新 · 译文待刷新" || stale.Chinese != translated.Chinese {
		t.Fatalf("stale translation shown as Final: %#v", stale)
	}
}

func TestMeetingSuggestionsUseCompletedSpeechOnly(t *testing.T) {
	if _, ok := LatestCompletedCaption([]Caption{{ID: 3, Source: "What about", Final: false}}); ok {
		t.Fatal("a partial fragment was used to generate suggestions")
	}
	finished, ok := LatestCompletedCaption([]Caption{
		{ID: 1, Source: "Let's consider both.", Final: true},
		{ID: 2, Source: "Maybe...", Final: false},
	})
	if !ok || finished.ID != 1 {
		t.Fatalf("suggestions ignored the latest completed turn: %#v %t", finished, ok)
	}
}

func TestCaptionVersionedTranslationAndHistoryReset(t *testing.T) {
	captions := UpsertCaption(nil, 4, "The date is Monday.", false, true)
	if !ApplyCaptionTranslation(captions, 4, "The date is Monday.", "日期是周一。") {
		t.Fatal("current translation was not applied")
	}
	captions = UpsertCaption(captions, 4, "The date is Tuesday.", false, true)
	if ApplyCaptionTranslation(captions, 4, "The date is Monday.", "过期译文") {
		t.Fatal("obsolete translation displaced latest recognition")
	}
	if captions[0].Chinese != "日期是周一。" || BuildReadingPanels(captions).Hint != "原文已更新 · 译文待刷新" {
		t.Fatalf("last readable translation must remain visible as stale: %+v", captions)
	}
	if !ApplyCaptionTranslation(captions, 4, "The date is Tuesday.", "日期是周二。") {
		t.Fatal("new translation was rejected")
	}
	captions = UpsertCaption(captions, 4, "The date is Tuesday.", true, true)
	captions = UpsertCaption(captions, 5, "unfinished", false, true)
	captions = KeepCompletedCaptions(captions)
	if len(captions) != 1 || captions[0].ID != 4 || captions[0].Chinese != "日期是周二。" {
		t.Fatalf("reset discarded finished history: %+v", captions)
	}
}

func TestCaptionStablePrefixAndFinalImmutability(t *testing.T) {
	captions := UpsertCaption(nil, 3, "Hello world", false, true)
	SetCaptionPrefix(captions, 3, "Hello ")
	captions = UpsertCaption(captions, 3, "Hello there", false, false)
	if captions[0].Stable != "Hello " || captions[0].Live != "there" {
		t.Fatalf("unstable prefix %v", captions[0])
	}
	captions = UpsertCaption(captions, 3, "Hello there!", true, true)
	captions = UpsertCaption(captions, 3, "stale preview", false, true)
	if !captions[0].Final || captions[0].Source != "Hello there!" {
		t.Fatalf("final caption revised by delayed result: %+v", captions[0])
	}
}

func TestBoundCaptionHistoryKeepsLongMeetingWithoutTwelveTurnCutoff(t *testing.T) {
	var captions []Caption
	for i := 0; i < 80; i++ {
		captions = append(captions, Caption{ID: uint64(i + 1), Source: "turn", Final: true})
	}
	kept, removed := BoundCaptionHistory(captions)
	if removed != 0 || len(kept) != 80 || BuildReadingPanels(kept).HistoryCount != 79 {
		t.Fatalf("80-turn conversation must remain readable: %d removed, %d kept", removed, len(kept))
	}
}

func TestBoundCaptionHistoryLimitsTurnsBytesAndPreservesUnicode(t *testing.T) {
	var captions []Caption
	for i := 0; i < 700; i++ {
		captions = append(captions, Caption{ID: uint64(i + 1), Source: "Hello", Chinese: "你好", Final: true})
	}
	kept, removed := BoundCaptionHistory(captions)
	if len(kept) != MaxHistoryCaptions || removed != 700-MaxHistoryCaptions || kept[len(kept)-1].ID != 700 {
		t.Fatalf("count limit: %d kept, %d removed", len(kept), removed)
	}
	big := strings.Repeat("你好", MaxCaptionField)
	captions = nil
	for i := 0; i < 20; i++ {
		captions = append(captions, Caption{ID: uint64(i + 1), Source: big, Chinese: big, Final: true})
	}
	kept, removed = BoundCaptionHistory(captions)
	bytes := 0
	for _, caption := range kept {
		bytes += len(caption.Source) + len(caption.Chinese) +
			len(caption.TranslatedFrom) + len(caption.Stable) + len(caption.Live)
		if !utf8.ValidString(caption.Source) || !utf8.ValidString(caption.Chinese) {
			t.Fatal("UTF-8 split at byte limit")
		}
	}
	if removed == 0 || bytes > MaxHistoryBytes || kept[len(kept)-1].ID != 20 {
		t.Fatalf("byte limit: %d kept, %d removed, %d bytes", len(kept), removed, bytes)
	}
}
