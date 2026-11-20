//go:build windows

package main

import (
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/xujinheng/windows-voice-rewrite/core/indicator"
)

// Run only on an interactive Windows desktop. Validates the native window
// lifetime, not its on-screen pixel-perfect similarity to the Tauri version.
func TestHandyStatusLifecycle(t *testing.T) {
	if os.Getenv("VOICE_REWRITE_NATIVE_STATUS_TEST") != "1" {
		t.Skip("requires interactive Windows desktop: VOICE_REWRITE_NATIVE_STATUS_TEST=1")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer closeHandyStatus()
	showHandyStatus(nil, "听写", indicator.Recording, 1)
	if activeHandyStatus == nil || activeHandyStatus.window == 0 {
		t.Fatal("could not create compact status")
	}
	if w := indicator.Width(activeHandyStatus.phase); w != indicator.RecordingWidth {
		t.Fatalf("recording width=%d", w)
	}
	updateHandyStatus(indicator.Processing)
	restoreDPI := handyAwareWindowThread()
	defer restoreDPI()
	var actual rect32
	user32.NewProc("GetWindowRect").Call(activeHandyStatus.window, uintptr(unsafe.Pointer(&actual)))
	required, _ := indicator.PhysicalSize(indicator.WorkingWidth, indicator.Height, activeHandyStatus.scale)
	if got := int(actual.Right - actual.Left); got != required {
		t.Fatalf("native HWND did not resize: got %d want %d", got, required)
	}
	if activeHandyStatus == nil || indicator.Width(activeHandyStatus.phase) != indicator.WorkingWidth {
		t.Fatal("working state not applied")
	}
	updateHandyStatus(indicator.Success)
	// Message pump dispatches WM_TIMER/WM_PAINT so the bar can expire.
	time.Sleep(1550 * time.Millisecond)
	for i := 0; i < 50 && activeHandyStatus != nil; i++ {
		var msg winMSG
		if have, _, _ := procPeekMessageForTest.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1); have != 0 {
			dispatchNativeMessage(&msg)
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if activeHandyStatus != nil {
		t.Fatal("status did not dismiss")
	}
}
