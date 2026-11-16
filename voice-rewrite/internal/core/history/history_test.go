package history

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionHistoryLabelsFollowTheLimit(t *testing.T) {
	title := SectionTitle()
	if !strings.Contains(title, fmt.Sprint(Limit)) || EmptySession == "" || ClearSession == "" {
		t.Fatalf("labels %q %q %q", title, EmptySession, ClearSession)
	}
}

func TestHistoryBoundedNewestFirstAndPrivate(t *testing.T) {
	var h Store
	for i := 0; i < 14; i++ {
		h.Remember("听写结果", fmt.Sprintf("内容%02d", i))
	}
	entries := h.Snapshot()
	if len(entries) != Limit || entries[0].Text != "内容13" || entries[9].Text != "内容04" {
		t.Fatalf("unexpected history: %#v", entries)
	}
	first, found := h.Get(entries[0].ID)
	if !found || first.Text != entries[0].Text {
		t.Fatal("cannot recover latest result by ID")
	}
	if _, found := h.Get(1); found {
		t.Fatal("evicted result still accessible")
	}
	h.Remember("", "   ")
	if len(h.Snapshot()) != Limit {
		t.Fatal("blank result was remembered")
	}
	h.Clear()
	if len(h.Snapshot()) != 0 {
		t.Fatal("clear did not erase session history")
	}
}

func TestHistoryNeverTruncatesSensitiveOutput(t *testing.T) {
	var h Store
	large := strings.Repeat("x", MaxBytes+1)
	h.Remember("大结果", large)
	if len(h.Snapshot()) != 0 {
		t.Fatal("large result must not be stored partially")
	}
	h.Remember("small", "hello")
	for i := 0; i < 5; i++ {
		h.Remember("large", strings.Repeat("z", 1<<20))
	}
	got := h.Snapshot()
	if len(got) > 4 || got[0].Text != strings.Repeat("z", 1<<20) {
		t.Fatalf("unexpected memory cap: %d", len(got))
	}
}

func TestHistoryConcurrentUse(t *testing.T) {
	var h Store
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				h.Remember("test", fmt.Sprintf("%d:%d", i, n))
				_ = h.Snapshot()
			}
		}(i)
	}
	wg.Wait()
	if len(h.Snapshot()) != Limit {
		t.Fatal("concurrent writers lost history capacity")
	}
}

func TestMenuLineHidesTranscript(t *testing.T) {
	created := time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local)
	dictation := MenuLine(0, Entry{Title: "听写结果", Text: "不要出现在菜单里", CreatedAt: created})
	answer := MenuLine(2, Entry{Title: "AI translate", Text: "secret answer", CreatedAt: created})
	if dictation != "1. 听写 · 15:04" || answer != "3. AI 结果 · 15:04" {
		t.Fatalf("menu lines: %q %q", dictation, answer)
	}
	if strings.Contains(dictation, "不要") || strings.Contains(answer, "secret") {
		t.Fatal("menu line included transcript text")
	}
}
