//go:build windows

package main

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// A kernel mutex cannot get stale after a crash, unlike a lock file.
func acquireLaunchLock() (func(), error) {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	create := kernel.NewProc("CreateMutexW")
	wait := kernel.NewProc("WaitForSingleObject")
	release := kernel.NewProc("ReleaseMutex")
	closeHandle := kernel.NewProc("CloseHandle")
	name, _ := syscall.UTF16PtrFromString("Local\\VoiceRewrite-Qwen3-ASR-OneClick")
	handle, _, err := create.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, fmt.Errorf("CreateMutexW: %w", err)
	}
	result, _, _ := wait.Call(handle, 0)
	if result == 0x102 { // WAIT_TIMEOUT: another OneClick is active.
		closeHandle.Call(handle)
		return nil, errors.New("Qwen3-ASR 正在安装或启动，请勿重复打开")
	}
	if result != 0 && result != 0x80 { // WAIT_OBJECT_0 or WAIT_ABANDONED
		closeHandle.Call(handle)
		return nil, fmt.Errorf("WaitForSingleObject: %d", result)
	}
	return func() {
		release.Call(handle)
		closeHandle.Call(handle)
	}, nil
}
