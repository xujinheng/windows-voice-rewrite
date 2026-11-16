// Package history keeps the current session's voice results in memory.
// It never writes speech, prompts, or answers to disk. Menus show only the
// result category and time, not the transcript.
package history

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	Limit        = 10
	MaxBytes     = 4 << 20
	EmptySession = "当前会话还没有可恢复的语音结果。"
	ClearSession = "清空本次会话历史"
)

type Entry struct {
	ID        uint64
	Title     string
	Text      string
	CreatedAt time.Time
}

type Store struct {
	mu      sync.Mutex
	nextID  uint64
	entries []Entry
}

func (h *Store) Remember(title, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	// A result that does not fit stays on its original card. Never keep a prefix.
	if len(value) > MaxBytes {
		return
	}
	if strings.TrimSpace(title) == "" {
		title = "语音结果"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	h.entries = append([]Entry{{ID: h.nextID, Title: title, Text: value, CreatedAt: time.Now()}}, h.entries...)
	bytes := 0
	keep := 0
	for _, entry := range h.entries {
		if keep >= Limit || bytes+len(entry.Text) > MaxBytes {
			break
		}
		bytes += len(entry.Text)
		keep++
	}
	for i := keep; i < len(h.entries); i++ {
		h.entries[i] = Entry{}
	}
	h.entries = h.entries[:keep]
}

func (h *Store) Snapshot() []Entry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Entry(nil), h.entries...)
}

func (h *Store) Get(id uint64) (Entry, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, entry := range h.entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return Entry{}, false
}

func (h *Store) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.entries {
		h.entries[i] = Entry{}
	}
	h.entries = nil
}

func Kind(title string) string {
	if strings.Contains(title, "听写") {
		return "听写"
	}
	return "AI 结果"
}

func MenuLine(index int, entry Entry) string {
	return fmt.Sprintf("%d. %s · %s", index+1, Kind(entry.Title), entry.CreatedAt.Format("15:04"))
}

// SectionTitle is the session-history heading. The count is Limit.
func SectionTitle() string {
	return fmt.Sprintf("最近 %d 次结果（仅当前会话）", Limit)
}
