package contract

import (
	"github.com/xujinheng/windows-voice-rewrite/core/history"
	"github.com/xujinheng/windows-voice-rewrite/core/ux"
)

// TrayItem is one portable menu action; hosts only render it and dispatch the ID.
// Meeting and OS-only debug commands remain native integrations.
type TrayItem struct {
	ID       int        `json:"id"`
	Title    string     `json:"title"`
	ResultID uint64     `json:"resultID,omitempty"`
	Children []TrayItem `json:"children,omitempty"`
}

// TrayMenu is the single command/label/order model for both desktop clients.
// IDs intentionally preserve Windows' existing tray command values.
func TrayMenu(paused bool, recent []history.Entry) []TrayItem {
	pause := ux.PauseVoice
	if paused {
		pause = ux.ResumeVoice
	}
	items := []TrayItem{
		{ID: 6, Title: pause},
		{ID: 2, Title: "设置与连接检测…"},
		{ID: 3, Title: ux.ViewDiagnosticLog},
		{ID: 5, Title: "最近一次错误与解决方法"},
		{ID: 7, Title: "恢复最近一次语音结果"},
	}
	if len(recent) != 0 {
		h := TrayItem{ID: 100, Title: history.SectionTitle()}
		for i, entry := range recent {
			h.Children = append(h.Children, TrayItem{ID: 1000 + i, Title: history.MenuLine(i, entry), ResultID: entry.ID})
		}
		items = append(items, h, TrayItem{ID: 9, Title: history.ClearSession})
	}
	return append(items,
		TrayItem{ID: 4, Title: "关闭 AI 结果窗口"},
		TrayItem{ID: 8, Title: "检查更新…"},
		TrayItem{ID: 1, Title: "退出 VoiceRewrite"},
	)
}
