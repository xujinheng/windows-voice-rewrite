//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	meetingcore "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/meeting"
)

func meetingE2EFirstLine(control uintptr) uintptr {
	line, _, _ := procSendMessage.Call(control, 0x00CE, 0, 0) // EM_GETFIRSTVISIBLELINE
	return line
}

// The UI helper intentionally caps reads at 4096 characters. An E2E
// transcript assertion must instead inspect the full native EDIT contents.
func meetingE2EFullText(control uintptr) string {
	n, _, _ := procGetWindowTextLength.Call(control)
	buf := make([]uint16, n+1)
	uxGetWindowText.Call(control, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func meetingE2EWindowRect(t *testing.T, control uintptr) rect32 {
	t.Helper()
	var bounds rect32
	if ok, _, err := popupGetWindowRect.Call(control, uintptr(unsafe.Pointer(&bounds))); ok == 0 {
		t.Fatalf("GetWindowRect(%x): %v", control, err)
	}
	return bounds
}

func meetingE2EHistory(count int) []meetingcore.Caption {
	captions := make([]meetingcore.Caption, 0, count)
	for i := 1; i <= count; i++ {
		source := fmt.Sprintf("This is completed meeting turn number %03d. We will review it in detail.", i)
		chinese := fmt.Sprintf("这是第 %03d 段已确认的中文会议记录，需要保留阅读位置。", i)
		captions = append(captions, meetingcore.Caption{ID: uint64(i), Source: source, Chinese: chinese, TranslatedFrom: source, Final: true})
	}
	return captions
}

// Runs against the production Win32 window and EDIT controls, not a rendering
// mock. The dedicated Actions job supplies an interactive Windows desktop.
func TestMeetingWindowsE2E(t *testing.T) {
	if os.Getenv("VOICEREWRITE_MEETING_E2E") != "1" {
		t.Skip("requires the dedicated Windows GUI runner")
	}
	t.Setenv("APPDATA", t.TempDir())
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	view := meetingcore.View{SourceName: "Meeting E2E", Captions: meetingE2EHistory(65)}
	if err := showMeetingHUD(0, view); err != nil {
		t.Fatalf("open native Meeting HUD: %v", err)
	}
	defer closeMeetingHUD()
	p := activeMeetingHUD
	if p == nil || p.window == 0 || p.currentChinese == 0 || p.currentEnglish == 0 || p.captions == 0 {
		t.Fatal("native Meeting controls were not created")
	}
	if got := uxWinText(p.currentChinese); !strings.Contains(got, "第 065 段") {
		t.Fatalf("live Chinese not rendered into native EDIT: %q", got)
	}
	if got := uxWinText(p.currentEnglish); !strings.Contains(got, "number 065") {
		t.Fatalf("original English not rendered into native EDIT: %q", got)
	}
	zh, en := meetingE2EWindowRect(t, p.currentChinese), meetingE2EWindowRect(t, p.currentEnglish)
	if zh.Top >= en.Top || zh.Bottom > en.Top || zh.Bottom-zh.Top <= en.Bottom-en.Top {
		t.Fatalf("Chinese must be first and larger: ZH=%+v EN=%+v", zh, en)
	}
	if p.historyCount != 64 || !strings.Contains(meetingE2EFullText(p.captions), "number 001") {
		t.Fatalf("native conversation history missing earlier turns: %d", p.historyCount)
	}

	// Go to the top as a reader, then append a finished turn. The real
	// EM_GETFIRSTVISIBLELINE must remain unchanged and unread must increase.
	procSendMessage.Call(p.captions, 0x0115, 6, 0) // WM_VSCROLL / SB_TOP
	top := meetingE2EFirstLine(p.captions)
	view.Captions = append(view.Captions, meetingcore.Caption{
		ID: 66, Source: "One more actual utterance", Chinese: "再补充一段实际发言",
		TranslatedFrom: "One more actual utterance", Final: true,
	})
	updateMeetingHUD(view)
	if first := meetingE2EFirstLine(p.captions); first != top || p.followLatest || p.unreadHistory != 1 {
		t.Fatalf("reading position stolen after append: first=%d want=%d follow=%t unread=%d",
			first, top, p.followLatest, p.unreadHistory)
	}
	if !strings.Contains(meetingE2EFullText(p.captions), "number 065") {
		t.Fatal("previous active utterance did not become native history")
	}
	p.scrollToLatest()
	if !p.followLatest || p.unreadHistory != 0 || meetingE2EFirstLine(p.captions) <= top {
		t.Fatalf("return to latest failed: follow=%t unread=%d first=%d",
			p.followLatest, p.unreadHistory, meetingE2EFirstLine(p.captions))
	}

	// Real button routing, visibility, and in-place status transitions.
	procSendMessage.Call(p.window, 0x0111, meetingcore.ButtonCaptions, 0)
	if !p.captionsOnly {
		t.Fatal("native captions-only button did not toggle the view")
	}
	procSendMessage.Call(p.window, 0x0111, meetingcore.ButtonCaptions, 0)
	if p.captionsOnly {
		t.Fatal("native complete-view button did not restore tools")
	}
	procSendMessage.Call(p.window, 0x0111, meetingcore.ButtonLargerText, 0)
	if p.fontSize != 18 {
		t.Fatalf("native font-size action not applied: %d", p.fontSize)
	}

	// Simulate a real mouse drag across every normal-workspace divider and
	// read the resulting native EDIT geometry instead of a layout mock.
	for divider := 0; divider < 5; divider++ {
		if !p.splitterVisible[divider] {
			t.Fatalf("divider %d is missing in full workspace", divider)
		}
		before := p.paneHeights
		a, b, ok := meetingcore.DividerPair(divider, false, false)
		if !ok {
			t.Fatalf("divider %d cannot be dragged", divider)
		}
		startY := p.splitterY[divider]
		endY := startY - p.px(8)
		point := func(y int32) uintptr { return uintptr(uint16(y))<<16 | uintptr(uint16(p.px(20))) }
		procSendMessage.Call(p.window, 0x0201, 0, point(startY)) // WM_LBUTTONDOWN
		procSendMessage.Call(p.window, 0x0200, 0, point(endY))   // WM_MOUSEMOVE
		procSendMessage.Call(p.window, 0x0202, 0, point(endY))   // WM_LBUTTONUP
		if p.paneHeights[a] >= before[a] || p.paneHeights[b] <= before[b] || p.dragDivider != -1 {
			t.Fatalf("native divider %d did not resize adjacent panes: before=%v after=%v", divider, before, p.paneHeights)
		}
		for index := 0; index < meetingcore.PaneCount; index++ {
			if index != a && index != b && p.paneHeights[index] != before[index] {
				t.Fatalf("native drag of divider %d changed unrelated pane %d: %v -> %v", divider, index, before, p.paneHeights)
			}
		}
	}
	stored, err := meetingcore.LoadPreferences(meetingPreferencesPath())
	if err != nil || stored.Panes != p.paneWeights {
		t.Fatalf("dragged panel layout was not persisted: saved=%v current=%v err=%v", stored.Panes, p.paneWeights, err)
	}

	longEnglish := strings.Repeat("This is a complete, speakable English answer for the meeting. ", 15)
	view.Expression = meetingcore.Line{Kind: "SAY", English: longEnglish, Complete: true}
	view.ExpressionSource = "请告诉大家一个详细的回答"
	updateMeetingHUD(view)
	if !p.expressionExpanded || p.copyExpression != longEnglish || uxWinText(p.expression) != longEnglish {
		t.Fatal("new long spoken-English result did not expand and remain copyable")
	}
	history := meetingE2EWindowRect(t, p.captions)
	expression := meetingE2EWindowRect(t, p.expression)
	if history.Bottom > expression.Top || expression.Bottom <= expression.Top {
		t.Fatalf("history and expanded English overlap: history=%+v expression=%+v", history, expression)
	}

	view.ExpressionStatus = "英文生成失败：超时"
	view.ExpressionSource = "识别到新的中文，但生成失败"
	updateMeetingHUD(view)
	if p.copyExpression != longEnglish || uxWinText(p.expression) != longEnglish {
		t.Fatal("failed inference wiped out previously completed English")
	}
	if detail := uxWinText(p.expressionSource); !strings.Contains(detail, "超时") || !strings.Contains(detail, "识别到新的中文") {
		t.Fatalf("native error detail did not preserve recognized speech: %q", detail)
	}

	procSendMessage.Call(p.window, 0x0111, meetingcore.ButtonExpression, 0)
	if p.expressionExpanded {
		t.Fatal("native collapse-English action failed")
	}
	procSendMessage.Call(p.window, 0x0111, meetingcore.ButtonCaptions, 0)
	view.Expression = meetingcore.Line{Kind: "SAY", English: "A newly completed answer.", Complete: true}
	view.ExpressionStatus = ""
	updateMeetingHUD(view)
	if p.captionsOnly || !p.expressionExpanded || p.copyExpression != view.Expression.English {
		t.Fatal("new voice answer did not appear while reading captions only")
	}
	if p.splitterVisible[3] || !p.splitterVisible[2] || !p.splitterVisible[4] {
		t.Fatal("expanded English must expose only the active pane dividers")
	}
	procSendMessage.Call(p.window, 0x0111, meetingcore.ButtonCaptions, 0) // return to captions-only layout
	if !p.captionsOnly || !p.splitterVisible[0] || !p.splitterVisible[1] || p.splitterVisible[2] {
		t.Fatal("captions-only view did not keep the Chinese/English/history dividers")
	}
}
