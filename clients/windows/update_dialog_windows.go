//go:build windows

package main

import (
	"runtime"
	"syscall"
	"unsafe"

	"github.com/xujinheng/windows-voice-rewrite/core/update"
)

func askViewUpdate(version string) bool {
	text, _ := syscall.UTF16PtrFromString(update.Prompt(version))
	caption, _ := syscall.UTF16PtrFromString("VoiceRewrite · 更新可用")
	answer, _, _ := procMessageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(caption)), 0x00000004|0x00000040|0x00040000)
	runtime.KeepAlive(text)
	runtime.KeepAlive(caption)
	return answer == 6
}
