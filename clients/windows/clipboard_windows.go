//go:build windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/xujinheng/windows-voice-rewrite/core/clipboard"
	"github.com/xujinheng/windows-voice-rewrite/core/ux"
)

func waitKeysReleased(max time.Duration, recordingKey bool) bool {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		down := false
		for i, k := range [...]uintptr{VK_CONTROL, VK_MENU, VK_SHIFT, recordVirtualKey} {
			if i == 3 && !recordingKey {
				break
			}
			v, _, _ := procGetAsyncKeyState.Call(k)
			if v&0x8000 != 0 {
				down = true
				break
			}
		}
		if !down {
			if recordingKey {
				time.Sleep(60 * time.Millisecond)
			}
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func sendCombo(letter uint16, shift bool) error {
	keys := []winINPUT{{Type: INPUT_KEYBOARD, Key: winKEYBDINPUT{Vk: VK_CONTROL}}}
	if shift {
		keys = append(keys, winINPUT{Type: INPUT_KEYBOARD, Key: winKEYBDINPUT{Vk: VK_SHIFT}})
	}
	keys = append(keys,
		winINPUT{Type: INPUT_KEYBOARD, Key: winKEYBDINPUT{Vk: letter}},
		winINPUT{Type: INPUT_KEYBOARD, Key: winKEYBDINPUT{Vk: letter, Flags: KEYEVENTF_KEYUP}})
	if shift {
		keys = append(keys, winINPUT{Type: INPUT_KEYBOARD, Key: winKEYBDINPUT{Vk: VK_SHIFT, Flags: KEYEVENTF_KEYUP}})
	}
	keys = append(keys, winINPUT{Type: INPUT_KEYBOARD, Key: winKEYBDINPUT{Vk: VK_CONTROL, Flags: KEYEVENTF_KEYUP}})
	if unsafe.Sizeof(keys[0]) != 40 {
		return fmt.Errorf("INPUT layout invalid: %d", unsafe.Sizeof(keys[0]))
	}
	n, _, e := procSendInput.Call(uintptr(len(keys)), uintptr(unsafe.Pointer(&keys[0])), unsafe.Sizeof(keys[0]))
	runtime.KeepAlive(keys)
	if n != uintptr(len(keys)) {
		return fmt.Errorf("SendInput 失败(%d/%d): %v。管理员权限的目标程序可能不接受普通权限模拟键盘", n, len(keys), e)
	}
	return nil
}
func clipboardSeq() uintptr { n, _, _ := procGetClipboardSequenceNumber.Call(); return n }
func openClipboard(hwnd uintptr) error {
	for i := 0; i < clipboard.OpenAttempts; i++ {
		ok, _, _ := procOpenClipboard.Call(hwnd)
		if ok != 0 {
			return nil
		}
		time.Sleep(clipboard.OpenPause)
	}
	return errors.New(clipboard.OpenFailed)
}
func getClipboardText(hwnd uintptr) (string, uintptr, error) {
	if e := openClipboard(hwnd); e != nil {
		return "", 0, e
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(CF_UNICODETEXT)
	if h == 0 {
		return "", 0, nil
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", 0, errors.New(clipboard.ReadFailed)
	}
	defer procGlobalUnlock.Call(h)
	// Limit reads to the actual allocation instead of assuming a huge buffer.
	allocationSize, _, _ := procGlobalSize.Call(h)
	if !clipboard.TextBlockReadable(allocationSize) {
		return "", 0, errors.New(clipboard.TextLengthRejected)
	}
	data := unsafe.Slice((*uint16)(unsafe.Pointer(p)), int(allocationSize/2))
	n := 0
	for n < len(data) && data[n] != 0 {
		n++
	}
	if n == len(data) {
		return "", 0, errors.New(clipboard.TextLengthRejected)
	}
	text := string(utf16.Decode(data[:n]))
	return text, clipboardSeq(), nil
}

// Return the sequence while the clipboard is still open. A later external
// copy must never become the sequence our snapshot claims to own.
func setClipboardText(hwnd uintptr, s string) (uintptr, error) {
	if e := openClipboard(hwnd); e != nil {
		return 0, e
	}
	defer procCloseClipboard.Call()
	chars := utf16.Encode([]rune(s))
	chars = append(chars, 0)
	size := uintptr(len(chars) * 2)
	h, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, size)
	if h == 0 {
		return 0, errors.New(clipboard.AllocFailed)
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return 0, errors.New(clipboard.LockFailed)
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(chars)), chars)
	procGlobalUnlock.Call(h)
	if ok, _, _ := procEmptyClipboard.Call(); ok == 0 {
		procGlobalFree.Call(h)
		return 0, errors.New(clipboard.EmptyFailed)
	}
	if out, _, _ := procSetClipboardData.Call(CF_UNICODETEXT, h); out == 0 {
		procGlobalFree.Call(h)
		return 0, errors.New(clipboard.WriteFailed)
	}
	return clipboardSeq(), nil
}

func clipboardCopy(hwnd uintptr, timeout time.Duration, terminal bool) (string, uintptr, error) {
	old := clipboardSeq()
	if err := sendCombo('C', terminal); err != nil {
		return "", 0, err
	}
	deadline := time.Now().Add(timeout)
	changed := false
	for time.Now().Before(deadline) {
		if clipboardSeq() != old {
			changed = true
			s, sequence, e := getClipboardText(hwnd)
			if e == nil && strings.TrimSpace(s) != "" {
				return s, sequence, nil
			}
		}
		time.Sleep(clipboard.CopyPoll)
	}
	// Do not use the clipboard's previous content as a supposed selection.
	if !changed {
		return "", 0, errors.New(clipboard.CopyUnchanged)
	}
	return "", 0, errors.New(clipboard.CopyEmpty)
}
func isSameForegroundWindow(target uintptr) bool {
	if target == 0 {
		return false
	}
	foreground, _, _ := procGetForegroundWindow.Call()
	valid, _, _ := procIsWindow.Call(target)
	return valid != 0 && foreground == target
}

// Terminal scrollback is selectable but not an editable range. Never send plain
// Ctrl+C to a console: it can interrupt a foreground command.
func isTerminalWindow(hwnd uintptr) bool {
	switch windowClassName(hwnd) {
	case "cascadia_hosting_window_class", "consolewindowclass":
		return true
	}
	return false
}

// Copy only while the original editor is foreground. Some applications update the
// clipboard asynchronously, so retry without crossing application boundaries.
func copySelectionWithRetry(hwnd, target uintptr, cfg Config) (string, uintptr, error) {
	time.Sleep(clipboard.CopySettle) // Let mouse key-up and selection settle.
	if !waitKeysReleased(clipboard.CopyModifierWait, false) {
		return "", 0, errors.New(clipboard.CopyModifiersHeld)
	}
	terminal := isTerminalWindow(target)
	attempts := cfg.SelectionCopyAttempts
	if terminal {
		attempts = 1 // Ctrl+Shift+C is safe for Windows Terminal; do not repeat unknown console shortcuts.
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if !isSameForegroundWindow(target) {
			return "", 0, errors.New(clipboard.CopyWindowBefore)
		}
		// For normal editors, retry an ignored Ctrl+C sooner, reserving
		// the remaining budget for a slow clipboard response. No extra
		// delay on successful copies and no longer overall timeout.
		timeout := clipboard.AttemptTimeout(time.Duration(cfg.SelectionCopyTimeoutMS)*time.Millisecond, i, attempts)
		text, sequence, err := clipboardCopy(hwnd, timeout, terminal)
		if !isSameForegroundWindow(target) {
			return "", 0, errors.New(clipboard.CopyWindowDuring)
		}
		if err == nil && strings.TrimSpace(text) != "" {
			return text, sequence, nil
		}
		lastErr = err
		if i+1 < attempts {
			time.Sleep(time.Duration(100*(i+1)) * time.Millisecond)
		}
	}
	return "", 0, fmt.Errorf("复制选中文字失败（已尝试 %d 次）：%w", attempts, lastErr)
}

func captureSelection(hwnd uintptr, cfg Config) (selectedText, error) {
	var sel selectedText
	sel.TargetWindow, _, _ = procGetForegroundWindow.Call()
	if sel.TargetWindow == 0 {
		return sel, errors.New(clipboard.CaptureNoWindow)
	}
	backup, err := snapshotClipboard(hwnd)
	if err != nil {
		return sel, err
	}
	sel.ClipboardBackup = backup
	text, sequence, err := copySelectionWithRetry(hwnd, sel.TargetWindow, cfg)
	if err != nil {
		// Failed selection capture cannot prove that the latest clipboard
		// update came from our Ctrl+C, rather than another application.
		return sel, err
	}
	backup.markOwned(sequence)
	sel.Text = text
	return sel, nil
}

// A paste may have reached the editor even if subsequent native verification fails.
// Such an error must not enable a second manual Apply of the same result.
type pasteAttemptedError struct{ error }

func (e pasteAttemptedError) Unwrap() error { return e.error }

// Unknown editor types (Chrome/Edge/WeChat/VS Code and more) cannot be
// classified reliably via Win32 HWND. First verify the selected text and
// foreground window; then paste and check whether the same original selection
// remains. If it does, the surface likely refused the paste, so show a preview.
// This remains a heuristic: it cannot prove an application's content changed.
func replaceSelectionWithGuard(hwnd uintptr, sel selectedText, replacement string, cfg Config, verifyBrowser bool, valid func() bool) error {
	if isTerminalWindow(sel.TargetWindow) {
		return errors.New(ux.ReplaceSelectionChanged)
	}
	taskValid := func() bool { return valid == nil || valid() }
	if !taskValid() {
		return errors.New(ux.ReplaceExpired)
	}
	if !waitKeysReleased(3 * time.Second, true) {
		return errors.New(ux.ReplaceReleaseKeys)
	}
	if !isSameForegroundWindow(sel.TargetWindow) {
		return errors.New(ux.ReplaceWindowSwitched)
	}

	backup := sel.ClipboardBackup
	if backup == nil || backup.isFinished() {
		var err error
		backup, err = snapshotClipboard(hwnd)
		if err != nil {
			return err
		}
	} else if !backup.ownsCurrent() {
		return errors.New(ux.ReplaceClipboardChanged)
	}
	defer backup.restoreIfOwned(hwnd)

	// Recheck foreground and selection before mutating the editor. Each Ctrl+C
	// changes the clipboard; track its sequence as part of our own operation.
	beforeCopy := clipboardSeq()
	actual, copiedSequence, err := copySelectionWithRetry(hwnd, sel.TargetWindow, cfg)
	if err == nil {
		if copiedSequence != beforeCopy {
			backup.markOwned(copiedSequence)
		}
	} else if clipboardSeq() != beforeCopy {
		// After a failed Ctrl+C, the latest sequence may belong to another
		// process. Preserve it instead of restoring our old snapshot over it.
		backup.abandon()
	}
	if err != nil || actual != sel.Text {
		return errors.New(ux.ReplaceSelectionChanged)
	}
	if !taskValid() || !isSameForegroundWindow(sel.TargetWindow) {
		return errors.New(ux.ReplaceBeforePaste)
	}
	sequence, err := setClipboardText(hwnd, replacement)
	if err != nil {
		return err
	}
	backup.markOwned(sequence)
	if !taskValid() || !isSameForegroundWindow(sel.TargetWindow) {
		return errors.New(ux.ReplacePasteStopped)
	}
	if err := sendCombo('V', false); err != nil {
		return pasteAttemptedError{err}
	}
	time.Sleep(270 * time.Millisecond)
	if verifyBrowser {
		if !isSameForegroundWindow(sel.TargetWindow) {
			return pasteAttemptedError{errors.New(ux.ReplaceWindowAfterPaste)}
		}
		beforeProbe := clipboardSeq()
		stillSelected, probedSequence, verifyErr := clipboardCopy(hwnd, 470*time.Millisecond, false)
		if verifyErr == nil {
			if probedSequence != beforeProbe {
				backup.markOwned(probedSequence)
			}
		} else if clipboardSeq() != beforeProbe {
			// An unsuccessful probe cannot claim the latest clipboard update.
			backup.abandon()
		}
		if verifyErr == nil && stillSelected == sel.Text {
			return pasteAttemptedError{errors.New(ux.ReplacePasteRefused)}
		}
	}
	if err := backup.restoreIfOwned(hwnd); err != nil {
		return pasteAttemptedError{fmt.Errorf("%s: %w", ux.ReplaceRestoreFailed, err)}
	}
	return nil // Windows accepted Ctrl+V; target editor insertion remains best-effort.
}
