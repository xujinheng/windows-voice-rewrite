//go:build windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/clipboard"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/ux"
)

func waitKeysReleased(max time.Duration) bool {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		keys := []uintptr{VK_CONTROL, VK_MENU, VK_SHIFT, recordVirtualKey}
		down := false
		for _, k := range keys {
			v, _, _ := procGetAsyncKeyState.Call(k)
			if v&0x8000 != 0 {
				down = true
				break
			}
		}
		if !down {
			time.Sleep(60 * time.Millisecond)
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func sendCombo(letter uint16) error {
	keys := [4]winINPUT{
		{Type: INPUT_KEYBOARD, Key: winKEYBDINPUT{Vk: VK_CONTROL}},
		{Type: INPUT_KEYBOARD, Key: winKEYBDINPUT{Vk: letter}},
		{Type: INPUT_KEYBOARD, Key: winKEYBDINPUT{Vk: letter, Flags: KEYEVENTF_KEYUP}},
		{Type: INPUT_KEYBOARD, Key: winKEYBDINPUT{Vk: VK_CONTROL, Flags: KEYEVENTF_KEYUP}},
	}
	if unsafe.Sizeof(keys[0]) != 40 {
		return fmt.Errorf("INPUT layout invalid: %d", unsafe.Sizeof(keys[0]))
	}
	n, _, e := procSendInput.Call(uintptr(len(keys)), uintptr(unsafe.Pointer(&keys[0])), unsafe.Sizeof(keys[0]))
	runtime.KeepAlive(keys)
	if n != 4 {
		return fmt.Errorf("SendInput 失败(%d/4): %v。管理员权限的目标程序可能不接受普通权限模拟键盘", n, e)
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
func getClipboardText(hwnd uintptr) (string, bool, uintptr, error) {
	if e := openClipboard(hwnd); e != nil {
		return "", false, 0, e
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(CF_UNICODETEXT)
	if h == 0 {
		return "", false, 0, nil
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", false, 0, errors.New(clipboard.ReadFailed)
	}
	defer procGlobalUnlock.Call(h)
	// Limit reads to the actual allocation instead of assuming a huge buffer.
	allocationSize, _, _ := procGlobalSize.Call(h)
	if !clipboard.TextBlockReadable(allocationSize) {
		return "", false, 0, errors.New(clipboard.TextLengthRejected)
	}
	data := unsafe.Slice((*uint16)(unsafe.Pointer(p)), int(allocationSize/2))
	n := 0
	for n < len(data) && data[n] != 0 {
		n++
	}
	if n == len(data) {
		return "", false, 0, errors.New(clipboard.TextLengthRejected)
	}
	text := string(utf16.Decode(data[:n]))
	return text, true, clipboardSeq(), nil
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

func clipboardCopy(hwnd uintptr, timeout time.Duration) (string, uintptr, error) {
	old := clipboardSeq()
	if err := sendCombo('C'); err != nil {
		return "", 0, err
	}
	deadline := time.Now().Add(timeout)
	changed := false
	for time.Now().Before(deadline) {
		if clipboardSeq() != old {
			changed = true
			s, ok, sequence, e := getClipboardText(hwnd)
			if clipboard.SelectionReadable(e, ok, s) {
				return s, sequence, nil
			}
		}
		time.Sleep(clipboard.CopyPoll)
	}
	// Do not use the clipboard's previous content as a supposed selection.
	return "", 0, clipboard.CopyPollMissed(changed)
}
func isSameForegroundWindow(target uintptr) bool {
	if target == 0 {
		return false
	}
	foreground, _, _ := procGetForegroundWindow.Call()
	valid, _, _ := procIsWindow.Call(target)
	return valid != 0 && foreground == target
}

// Copy only while the original editor is foreground. Some applications update the
// clipboard asynchronously, so retry without crossing application boundaries.
// A mouse side-button may be released while Ctrl/Alt/Shift is still physically
// held. SendInput does not clear that state; an injected Ctrl+C could then be
// interpreted as a different shortcut. Normal mouse-only use returns at once.
func waitCopyModifiersReleased(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		down := false
		for _, key := range [...]uintptr{VK_CONTROL, VK_MENU, VK_SHIFT} {
			state, _, _ := procGetAsyncKeyState.Call(key)
			if state&0x8000 != 0 {
				down = true
				break
			}
		}
		if !down {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func copySelectionWithRetry(hwnd, target uintptr, cfg Config) (string, uintptr, error) {
	time.Sleep(clipboard.CopySettle) // Let mouse key-up and selection settle.
	if !waitCopyModifiersReleased(clipboard.CopyModifierWait) {
		return "", 0, errors.New(clipboard.CopyModifiersHeld)
	}
	var lastErr error
	for i := 0; i < cfg.SelectionCopyAttempts; i++ {
		if err := clipboard.CopyWindowReady(isSameForegroundWindow(target), false); err != nil {
			return "", 0, err
		}
		// Two-attempt default: retry an ignored Ctrl+C sooner, reserving
		// the remaining budget for a slow clipboard response. No extra
		// delay on successful copies and no longer overall timeout.
		timeout := clipboard.AttemptTimeout(time.Duration(cfg.SelectionCopyTimeoutMS)*time.Millisecond, i, cfg.SelectionCopyAttempts)
		text, sequence, err := clipboardCopy(hwnd, timeout)
		if windowErr := clipboard.CopyWindowReady(isSameForegroundWindow(target), true); windowErr != nil {
			return "", 0, windowErr
		}
		if clipboard.SelectionReadable(err, true, text) {
			return text, sequence, nil
		}
		lastErr = err
		if i+1 < cfg.SelectionCopyAttempts {
			time.Sleep(clipboard.CopyRetryPause(i))
		}
	}
	return "", 0, fmt.Errorf("复制选中文字失败（已尝试 %d 次）：%w", cfg.SelectionCopyAttempts, lastErr)
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

// Unknown editor types (Chrome/Edge/WeChat/VS Code and more) cannot be
// classified reliably via Win32 HWND. First verify the selected text and
// foreground window; then paste and check whether the same original selection
// remains. If it does, the surface likely refused the paste, so show a preview.
// This remains a heuristic: it cannot prove an application's content changed.
func replaceSelectionWithGuard(hwnd uintptr, sel selectedText, replacement string, cfg Config, verifyBrowser bool, valid func() bool) error {
	taskValid := func() bool { return valid == nil || valid() }
	if !taskValid() {
		return errors.New(ux.ReplaceExpired)
	}
	if !waitKeysReleased(3 * time.Second) {
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
	if err := sendCombo('V'); err != nil {
		return err
	}
	time.Sleep(270 * time.Millisecond)
	if verifyBrowser {
		if !isSameForegroundWindow(sel.TargetWindow) {
			return errors.New(ux.ReplaceWindowAfterPaste)
		}
		beforeProbe := clipboardSeq()
		stillSelected, probedSequence, verifyErr := clipboardCopy(hwnd, 470*time.Millisecond)
		if verifyErr == nil {
			if probedSequence != beforeProbe {
				backup.markOwned(probedSequence)
			}
		} else if clipboardSeq() != beforeProbe {
			// An unsuccessful probe cannot claim the latest clipboard update.
			backup.abandon()
		}
		if verifyErr == nil && stillSelected == sel.Text {
			return errors.New(ux.ReplacePasteRefused)
		}
	}
	if err := backup.restoreIfOwned(hwnd); err != nil {
		return fmt.Errorf("%s: %w", ux.ReplaceRestoreFailed, err)
	}
	return nil // Windows accepted Ctrl+V; target editor insertion remains best-effort.
}
