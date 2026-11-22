//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// The lock file and all downloaded data remain beside the launcher.
func acquireLaunchLock(directory string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(directory, ".oneclick.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil { return nil, err }
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("Qwen3-ASR 正在安装或启动: %w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
