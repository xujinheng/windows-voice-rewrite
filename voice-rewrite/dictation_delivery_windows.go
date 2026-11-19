//go:build windows

package main

import (
	"errors"
	"fmt"
	"time"

	"voicerewrite/internal/core/clipboard"
	"voicerewrite/internal/core/ux"
)

// Native Windows delivery uses the same user-visible Mouse4 behavior.
// Dictation preserves multi-format clipboard contents. If a format cannot be
// backed up safely, use direct Unicode input rather than changing clipboard.
func insertDictationText(hwnd, target uintptr, text string, valid func() bool) error {
	windowOK := func() bool { return valid() && isSameForegroundWindow(target) }
	if err := ux.DictationInsertReady(text, func() bool { return waitKeysReleased(2 * time.Second) }, windowOK, func() bool { return nativePasswordFieldFocused(target) }); err != nil {
		return err
	}
	backup, err := snapshotClipboard(hwnd)
	if err != nil {
		// High-quality clipboard support must never destroy a bitmap, virtual
		// file, or an application's private object merely to paste text.
		return sendUnicodeText(text, func() bool { return valid() && isSameForegroundWindow(target) })
	}
	if err = setClipboardText(hwnd, text); err != nil {
		return err
	}
	backup.markOwned(clipboardSeq())
	defer backup.restoreIfOwned(hwnd)
	if !windowOK() {
		return errors.New(ux.DictationWindowChanged)
	}
	if err = sendCombo('V'); err != nil {
		return err
	}
	time.Sleep(clipboard.WindowsPasteWaitMilliseconds * time.Millisecond)
	if err = backup.restoreIfOwned(hwnd); err != nil {
		return fmt.Errorf("%s: %w", ux.DictationRestoreFailed, err)
	}
	return nil // SendInput accepted; insertion cannot be proven in all editors.
}
