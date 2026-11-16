// Package logfile rotates a diagnostic log before it is opened.
// The active file and a fixed number of numbered backups stay on disk.
package logfile

import (
	"fmt"
	"os"
)

const (
	MaxBytes = 4 << 20
	Backups  = 3
)

// Rotate shifts path into path.1 .. path.N when it reaches maxBytes.
// A missing file is not an error. Call it before opening the active log.
func Rotate(path string, maxBytes int64, backups int) error {
	if backups < 1 || maxBytes < 1 {
		return fmt.Errorf("invalid log rotation policy")
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("log path is a directory: %s", path)
	}
	if info.Size() < maxBytes {
		return nil
	}
	for i := backups; i > 1; i-- {
		previous := fmt.Sprintf("%s.%d", path, i-1)
		next := fmt.Sprintf("%s.%d", path, i)
		if _, err := os.Stat(previous); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := os.Remove(next); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(previous, next); err != nil {
			return err
		}
	}
	oldest := path + ".1"
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(path, oldest)
}
