//go:build windows

package main

import (
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/voice"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/ux"
)

var (
	procPeekMessageForTest = user32.NewProc("PeekMessageW")
	procPostMessageForTest = user32.NewProc("PostMessageW")
)

// Enable with VOICE_REWRITE_NATIVE_POPUP_TEST=1 on an interactive Windows desktop.
// The isolated Linux tests and Windows cross-build cannot exercise GUI messages.
func TestNativePopupPaintAndClose(t *testing.T) {
	if os.Getenv("VOICE_REWRITE_NATIVE_POPUP_TEST") != "1" {
		t.Skip("requires an interactive Windows desktop; VOICE_REWRITE_NATIVE_POPUP_TEST=1")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer closeActiveAnswer()
	if err := showAnswerPopup(popupMessage{title: "测试", body: "这是一段正常显示的中文答案。", anchor: trayPoint{X: 350, Y: 350}}); err != nil {
		t.Fatal(err)
	}
	if activeAnswer == nil || activeAnswer.window == 0 || activeAnswer.edit == 0 {
		t.Fatal("popup not created")
	}
	if n, _, _ := procGetWindowTextLength.Call(activeAnswer.edit); n == 0 {
		t.Fatal("popup content is blank")
	}
	hwnd := activeAnswer.window
	if ok, _, err := procPostMessageForTest.Call(hwnd, WM_CLOSE, 0, 0); ok == 0 {
		t.Fatalf("PostMessage close: %v", err)
	}
	for i := 0; i < 100 && activeAnswer != nil; i++ {
		var msg winMSG
		if have, _, _ := procPeekMessageForTest.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1); have != 0 {
			dispatchNativeMessage(&msg)
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if activeAnswer != nil {
		t.Fatal("WM_CLOSE was not dispatched to the window procedure")
	}
	// Button WM_COMMAND must also close the window.
	if err := showAnswerPopup(popupMessage{title: "翻译", body: "Hello, world!", anchor: trayPoint{X: 350, Y: 350}}); err != nil {
		t.Fatal(err)
	}
	hwnd = activeAnswer.window
	procSendMessage.Call(hwnd, WM_COMMAND, BUTTON_CLOSE, 0)
	if activeAnswer != nil {
		t.Fatal("Close button failed")
	}
}

// This exercises the real native failure notification without a microphone or
// interactive editor. A missing target must release busy and notify exactly the
// card that started Apply; the Win32 pump rejects a zero/stale card handle.
func TestClosedEditorApplyPostsCardIdentity(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd, err := createTrayCallbackWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer procDestroyWindow.Call(hwnd)
	app := &appState{Window: hwnd}
	generation := app.core.Apply(voice.Command{Kind: voice.CommandStart}).Token
	defer app.core.Apply(voice.Command{Kind: voice.CommandShutdown})
	card := &answerPopup{app: app, window: hwnd, generation: generation, allowApply: true, selection: &selectedText{}}
	card.apply()
	if app.busy.Load() {
		t.Fatal("closed editor retained the apply busy lease")
	}
	select {
	case got := <-card.applyResult:
		if got == nil || got.Error() != ux.ApplyWindowClosed {
			t.Fatalf("apply result = %v; want closed-editor failure", got)
		}
	default:
		t.Fatal("closed editor left no apply outcome")
	}
	var msg winMSG
	if got, _, _ := procPeekMessageForTest.Call(uintptr(unsafe.Pointer(&msg)), hwnd, WM_APPLY_RESULT, WM_APPLY_RESULT, 1); got == 0 {
		t.Fatal("closed editor did not notify the Win32 UI pump")
	}
	if msg.WParam != card.window {
		t.Fatalf("apply notification card = %#x; want %#x", msg.WParam, card.window)
	}
}

// Verify Win32 DRAWITEMSTRUCT's x64 layout before owner-draw button callbacks.
func TestDrawItemLayoutX64(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) == 8 && unsafe.Sizeof(drawItemStruct{}) != 64 {
		t.Fatalf("DRAWITEMSTRUCT layout invalid: %d", unsafe.Sizeof(drawItemStruct{}))
	}
}
