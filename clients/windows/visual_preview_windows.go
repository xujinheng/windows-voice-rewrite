//go:build windows

package main

import (
	"os"
	"runtime"
	"unsafe"

	"github.com/xujinheng/windows-voice-rewrite/core/indicator"
)

// runRecordingVisualPreview displays the real GDI+ layered recording overlay
// without opening a microphone or sending audio to a provider. Used by the
// Windows screenshot CI job; never enabled in ordinary launches.
func runRecordingVisualPreview() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// These fixed states use the native Win32 painter. They do not start
	// microphone capture, request permission or perform model inference.
	switch os.Getenv("VOICEREWRITE_VISUAL_STATE") {
	case "preview":
		showPendingHandyStatus("听写")
	case "processing":
		showHandyStatus(nil, "听写", indicator.Processing, 0x56525348)
	case "failure":
		showHandyStatus(nil, "听写", indicator.Error, 0x56525348)
	case "success-event-sent":
		showHandyStatus(nil, "听写", indicator.Success, 0x56525348)
	default:
		showHandyStatus(nil, "听写", indicator.Recording, 0x56525348)
	}
	defer closeHandyStatus()
	if activeHandyStatus == nil {
		return
	}
	for {
		var msg winMSG
		rv, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(rv) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// Real settings controls and paints, no microphone/model calls or config writes.
func runSettingsVisualPreview() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := &appState{cfg: defaults()}
	if err := showNativeSettings(app); err != nil {
		return
	}
	defer closeNativeSettings()
	for {
		var msg winMSG
		rv, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(rv) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
