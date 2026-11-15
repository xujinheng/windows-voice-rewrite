//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

func meetingE2EFirstLine(control uintptr) uintptr {
	line, _, _ := procSendMessage.Call(control, 0x00CE, 0, 0) // EM_GETFIRSTVISIBLELINE
	return line
}

func meetingE2EWindowRect(t *testing.T, control uintptr) rect32 {
	t.Helper()
	var bounds rect32
	if ok, _, err := popupGetWindowRect.Call(control, uintptr(unsafe.Pointer(&bounds))); ok == 0 {
		t.Fatalf("GetWindowRect(%x): %v", control, err)
	}
	return bounds
}

func meetingE2EHistory(count int) []meetingCaption {
	captions := make([]meetingCaption, 0, count)
	for i := 1; i <= count; i++ {
		source := fmt.Sprintf("This is completed meeting turn number %03d. We will review it in detail.", i)
		chinese := fmt.Sprintf("这是第 %03d 段已确认的中文会议记录，需要保留阅读位置。", i)
		captions = append(captions, meetingCaption{id: uint64(i), source: source, chinese: chinese, translatedFrom: source, final: true})
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

	view := meetingView{sourceName: "Meeting E2E", captions: meetingE2EHistory(65)}
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
	if p.historyCount != 64 || !strings.Contains(uxWinText(p.captions), "number 001") {
		t.Fatalf("native conversation history missing earlier turns: %d", p.historyCount)
	}

	// Go to the top as a reader, then append a finished turn. The real
	// EM_GETFIRSTVISIBLELINE must remain unchanged and unread must increase.
	procSendMessage.Call(p.captions, 0x0115, 6, 0) // WM_VSCROLL / SB_TOP
	top := meetingE2EFirstLine(p.captions)
	view.captions = append(view.captions, meetingCaption{
		id: 66, source: "One more actual utterance", chinese: "再补充一段实际发言",
		translatedFrom: "One more actual utterance", final: true,
	})
	updateMeetingHUD(view)
	if first := meetingE2EFirstLine(p.captions); first != top || p.followLatest || p.unreadHistory != 1 {
		t.Fatalf("reading position stolen after append: first=%d want=%d follow=%t unread=%d",
			first, top, p.followLatest, p.unreadHistory)
	}
	if !strings.Contains(uxWinText(p.captions), "number 065") {
		t.Fatal("previous active utterance did not become native history")
	}
	p.scrollToLatest()
	if !p.followLatest || p.unreadHistory != 0 || meetingE2EFirstLine(p.captions) <= top {
		t.Fatalf("return to latest failed: follow=%t unread=%d first=%d",
			p.followLatest, p.unreadHistory, meetingE2EFirstLine(p.captions))
	}

	// Real button routing, visibility, and in-place status transitions.
	procSendMessage.Call(p.window, 0x0111, 7101, 0) // WM_COMMAND: 专注字幕
	if !p.captionsOnly {
		t.Fatal("native captions-only button did not toggle the view")
	}
	procSendMessage.Call(p.window, 0x0111, 7101, 0)
	if p.captionsOnly {
		t.Fatal("native complete-view button did not restore tools")
	}
	procSendMessage.Call(p.window, 0x0111, 7103, 0) // A+
	if p.fontSize != 18 {
		t.Fatalf("native font-size action not applied: %d", p.fontSize)
	}

	longEnglish := strings.Repeat("This is a complete, speakable English answer for the meeting. ", 15)
	view.expression = meetingLine{kind: "SAY", english: longEnglish, complete: true}
	view.expressionSource = "请告诉大家一个详细的回答"
	updateMeetingHUD(view)
	if !p.expressionExpanded || p.copyExpression != longEnglish || uxWinText(p.expression) != longEnglish {
		t.Fatal("new long spoken-English result did not expand and remain copyable")
	}
	history := meetingE2EWindowRect(t, p.captions)
	expression := meetingE2EWindowRect(t, p.expression)
	if history.Bottom > expression.Top || expression.Bottom <= expression.Top {
		t.Fatalf("history and expanded English overlap: history=%+v expression=%+v", history, expression)
	}

	view.expressionStatus = "英文生成失败：超时"
	view.expressionSource = "识别到新的中文，但生成失败"
	updateMeetingHUD(view)
	if p.copyExpression != longEnglish || uxWinText(p.expression) != longEnglish {
		t.Fatal("failed inference wiped out previously completed English")
	}
	if detail := uxWinText(p.expressionSource); !strings.Contains(detail, "超时") || !strings.Contains(detail, "识别到新的中文") {
		t.Fatalf("native error detail did not preserve recognized speech: %q", detail)
	}

	procSendMessage.Call(p.window, 0x0111, 7115, 0) // 收起英文
	if p.expressionExpanded {
		t.Fatal("native collapse-English action failed")
	}
	procSendMessage.Call(p.window, 0x0111, 7101, 0) // 专注字幕
	view.expression = meetingLine{kind: "SAY", english: "A newly completed answer.", complete: true}
	view.expressionStatus = ""
	updateMeetingHUD(view)
	if p.captionsOnly || !p.expressionExpanded || p.copyExpression != view.expression.english {
		t.Fatal("new voice answer did not appear while reading captions only")
	}
}
