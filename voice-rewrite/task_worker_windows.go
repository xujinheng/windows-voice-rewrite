//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"

	"voicerewrite/internal/core/audio"
	"voicerewrite/internal/core/clipboard"
	"voicerewrite/internal/core/indicator"
	"voicerewrite/internal/core/input"
	"voicerewrite/internal/core/speech"
	"voicerewrite/internal/core/textedit"
	"voicerewrite/internal/core/ux"
)

func watchMouseHoldTimeout(hwnd, session uintptr, canceled <-chan struct{}, timeout time.Duration, logger *log.Logger) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-canceled:
		return
	case <-timer.C:
		logger.Printf("mouse hold timed out; session=%d", session)
		if ok, _, err := procPostMessage.Call(hwnd, WM_HOLD_TIMEOUT, session, 0); ok == 0 {
			logger.Printf("ERROR: cannot post mouse timeout: %v", err)
		}
	}
}

func watchHoldRelease(hwnd, vk, mods, session uintptr, canceled <-chan struct{}, timeout time.Duration, logger *log.Logger) {
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	start := time.Now()
	releasedTicks := 0
	polls := 0
	logger.Printf("hold watcher started; session=%d", session)
	for {
		select {
		case <-canceled:
			return
		case <-deadline.C:
			logger.Printf("hold watcher deadline reached; session=%d", session)
			if ok, _, err := procPostMessage.Call(hwnd, WM_HOLD_TIMEOUT, session, 0); ok == 0 {
				logger.Printf("ERROR: cannot post hold timeout: %v", err)
			}
			return
		case <-tick.C:
			pressedValue, _, _ := procGetAsyncKeyState.Call(vk)
			polls++
			if polls == 40 || polls%80 == 0 {
				logger.Printf("hold watcher state; session=%d down=%t elapsed=%s", session, pressedValue&0x8000 != 0, time.Since(start).Truncate(time.Millisecond))
			}
			// A chord must be fully released before we process text/clipboard:
			// holding Ctrl during a simulated Ctrl+C/V would break editing.
			down := pressedValue&0x8000 != 0
			for _, required := range []struct{ flag, vk uintptr }{
				{MOD_CTRL, VK_CONTROL}, {MOD_ALT, VK_MENU}, {MOD_SHIFT, VK_SHIFT},
			} {
				if mods&required.flag != 0 {
					state, _, _ := procGetAsyncKeyState.Call(required.vk)
					down = down || state&0x8000 != 0
				}
			}
			if input.ReleaseDebounced(down, &releasedTicks) {
				logger.Printf("hold watcher detected key release; session=%d elapsed=%s", session, time.Since(start).Truncate(time.Millisecond))
				if ok, _, err := procPostMessage.Call(hwnd, WM_HOLD_RELEASE, session, 0); ok == 0 {
					logger.Printf("ERROR: cannot post hold release: %v", err)
				}
				return
			}
		}
	}
}

// Restore every snapshotted clipboard format when the sequence is still ours.
// Never overwrite a later clipboard change made by the user or another app.
func restoreSelectionClipboardIfOwned(hwnd uintptr, sel selectedText) {
	if sel.ClipboardBackup != nil {
		_ = sel.ClipboardBackup.restoreIfOwned(hwnd)
		return
	}
	if clipboard.MayRestore(sel.HadOriginalClipboard, uint64(sel.CopyClipboardSeq), uint64(clipboardSeq())) {
		_ = setClipboardText(hwnd, sel.OriginalClipboard)
	}
}

// Mouse4 never performs Ctrl+C or model-based intent routing. Handy-style
// dictation inserts raw ASR output at the captured window's current caret.
func processDictationHold(app *appState, rec *audioRecorder, expectedWindow uintptr, generation uint64) {
	defer app.busy.Store(false)
	defer func() {
		if x := recover(); x != nil {
			app.logger.Printf("dictation panic: %v", x)
			app.fail(fmt.Errorf("听写处理失败: %v", x))
			app.postHandyPhase(indicator.Error, generation)
		}
	}()
	r := <-rec.result
	if !app.epoch.Valid(generation) {
		return
	}
	if err := ux.DictationCaptureProblem(r.err, audio.BelowMinCapture(r.pcm)); err != nil {
		app.fail(err)
		app.postHandyPhase(indicator.Error, generation)
		return
	}
	ctx, cancel := context.WithTimeout(app.epoch.ContextFor(generation), 220*time.Second)
	defer cancel()
	start := time.Now()
	text, err := speech.Transcribe(ctx, app.cfg, r.pcm)
	if !app.epoch.Valid(generation) {
		return
	}
	app.logger.Printf("stage=asr flow=dictation ms=%d", time.Since(start).Milliseconds())
	if err != nil {
		app.fail(err)
		app.postHandyPhase(indicator.Error, generation)
		return
	}
	if !app.epoch.Valid(generation) {
		return
	}
	// Focus may have changed while ASR ran: never insert in another app.
	if !isSameForegroundWindow(expectedWindow) {
		// A result card preserves the transcript without requiring the user to
		// guess what happened to their clipboard after a focus change.
		follow := ux.DictationUnfocused()
		app.rememberResult(ux.DictationHistoryTitle, text)
		app.showAnswer(follow.Title, text, currentCursorPoint(), nil, false, generation)
		app.postHandyPhase(follow.Phase, generation)
		return
	}
	start = time.Now()
	err = insertDictationText(app.Window, expectedWindow, text, func() bool { return app.epoch.Valid(generation) })
	follow := ux.DictationInserted(err)
	if follow.ShowCard {
		app.fail(err)
		app.rememberResult(ux.DictationHistoryTitle, text)
		app.showAnswer(follow.Title, text, currentCursorPoint(), nil, false, generation)
		app.postHandyPhase(follow.Phase, generation)
		return
	}
	app.rememberResult(ux.DictationHistoryTitle, text)
	app.logger.Printf("stage=paste flow=dictation ms=%d chars=%d status=unverified", time.Since(start).Milliseconds(), utf8.RuneCountInString(text))
	app.setStatus(traySuccess)
	app.postHandyPhase(follow.Phase, generation)
}

// Dictation preserves multi-format clipboard contents. If a format cannot be
// backed up safely, use direct Unicode input rather than changing clipboard.
func insertDictationText(hwnd, target uintptr, text string, valid func() bool) error {
	if strings.TrimSpace(text) == "" {
		return errors.New(ux.DictationEmpty)
	}
	if !waitKeysReleased(2 * time.Second) {
		return errors.New("请松开快捷键后重试")
	}
	windowChanged := errors.New("写入前窗口已变化，已取消听写")
	if !valid() || !isSameForegroundWindow(target) {
		return windowChanged
	}
	if nativePasswordFieldFocused(target) {
		return errors.New(ux.PasswordDictation)
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
	if !valid() || !isSameForegroundWindow(target) {
		return windowChanged
	}
	if err = sendCombo('V'); err != nil {
		return err
	}
	time.Sleep(250 * time.Millisecond)
	if err = backup.restoreIfOwned(hwnd); err != nil {
		return fmt.Errorf("已触发粘贴，但剪贴板恢复失败: %w", err)
	}
	return nil // SendInput accepted; insertion cannot be proven in all editors.
}

// The selection must be copied AFTER releasing the held shortcut. This function
// runs off the UI and low-level mouse-hook threads, and enforces a small time budget.
func processRecordedHold(app *appState, rec *audioRecorder, expectedWindow uintptr, anchor trayPoint, generation uint64) {
	started := time.Now()
	if !app.epoch.Valid(generation) {
		rec.Stop()
		go func() { <-rec.result }()
		return
	}
	defer app.busy.Store(false)
	defer func() {
		if reason := recover(); reason != nil {
			app.logger.Printf("hold worker panic: %v\n%s", reason, debug.Stack())
			app.fail(fmt.Errorf("录音处理线程发生错误：%v", reason))
		}
	}()
	if !isSameForegroundWindow(expectedWindow) {
		rec.Stop()
		go func() { <-rec.result }()
		app.fail(errors.New("编辑窗口已经变化，取消自动替换"))
		return
	}
	sel, err := captureSelection(app.Window, app.cfg)
	sel.Anchor = anchor
	defer restoreSelectionClipboardIfOwned(app.Window, sel)
	app.logger.Printf("stage=selection mode=hold ms=%d", time.Since(started).Milliseconds())
	if err == nil && sel.TargetWindow != expectedWindow {
		err = errors.New("选区所属窗口已变化")
	}
	if err == nil {
		err = textedit.SelectedLengthError(app.cfg.MaxSelectedRunes, sel.Text)
	}
	if err != nil {
		go func() { <-rec.result }()
		app.fail(err)
		return
	}
	app.logger.Printf("selection captured; chars=%d", utf8.RuneCountInString(sel.Text))
	app.postHandyPhase(indicator.Processing, generation)
	if !app.epoch.Valid(generation) {
		return
	}
	// We already own the busy flag and error recovery: avoid a second outer worker.
	processAudioAndRewrite(app, rec, sel, generation)
}

func processRecorded(app *appState, rec *audioRecorder, sel selectedText, generation uint64) {
	defer app.busy.Store(false)
	defer restoreSelectionClipboardIfOwned(app.Window, sel)
	defer func() {
		if reason := recover(); reason != nil {
			app.logger.Printf("worker panic: %v\n%s", reason, debug.Stack())
			app.fail(fmt.Errorf("录音处理线程发生错误：%v", reason))
		}
	}()
	processAudioAndRewrite(app, rec, sel, generation)
}

func processAudioAndRewrite(app *appState, rec *audioRecorder, sel selectedText, generation uint64) {
	if !app.epoch.Valid(generation) {
		return
	}
	app.logger.Print("worker: waiting for recording buffer")
	r := <-rec.result
	if !app.epoch.Valid(generation) {
		return
	}
	if err := ux.RewriteCaptureProblem(r.err, audio.BelowMinCapture(r.pcm)); err != nil {
		app.fail(err)
		return
	}
	app.logger.Printf("audio capture complete; %.2f seconds", float64(len(r.pcm))/audio.PCMBytesPerSecond)
	ctx, cancel := context.WithTimeout(app.epoch.ContextFor(generation), 240*time.Second)
	defer cancel()
	started := time.Now()
	answer, instruction, e := textedit.ResolveSpokenTask(ctx, app.cfg, sel.Text, r.pcm, func(text string) {
		if !app.epoch.Valid(generation) {
			return
		}
		app.logger.Printf("stage=asr ms=%d", time.Since(started).Milliseconds())
		app.setStatus(trayRewriting)
		app.postHandyPhase(indicator.Rewriting, generation)
		app.logger.Printf("ASR instruction decoded; chars=%d", utf8.RuneCountInString(text))
	})
	if !app.epoch.Valid(generation) {
		return
	}
	app.logger.Printf("stage=spoken_selection ms=%d intent=%s", time.Since(started).Milliseconds(), answer.Intent)
	if e != nil {
		app.fail(e)
		return
	}
	editable := isSafelyEditableControl(sel.TargetWindow)
	browserSurface := isBrowserEditorSurface(sel.TargetWindow)
	action := textedit.RouteTask(answer, instruction, editable)
	app.logger.Printf("route=%s intent=%s native_editable=%t browser_surface=%t", action, answer.Intent, editable, browserSurface)
	allowApply := action == textedit.Replace || action == textedit.TryReplace
	app.rememberResult(ux.HistoryTitle(answer.Intent), answer.Result)
	if allowApply {
		app.postHandyPhase(ux.RewriteApplying().Phase, generation)
		startedPaste := time.Now()
		// All unknown controls receive the same pre/post paste checks as a browser.
		e = replaceSelectionWithGuard(app.Window, sel, answer.Result, app.cfg, action == textedit.TryReplace, func() bool { return app.epoch.Valid(generation) })
		app.logger.Printf("stage=paste ms=%d", time.Since(startedPaste).Milliseconds())
	}
	follow := ux.RewritePasted(allowApply, e)
	if !follow.ShowCard {
		app.setStatus(traySuccess)
		app.postHandyPhase(follow.Phase, generation)
		// Completion is indicated by the non-blocking status bar; no sound.
		app.logger.Printf("paste_status=unverified intent=%s result_chars=%d (SendInput accepted, editor insertion not provable)", answer.Intent, utf8.RuneCountInString(answer.Result))
		return
	}
	if allowApply && e != nil {
		app.logger.Printf("auto replacement failed; preview instead: %v", e)
	}
	title := ux.ResultCardTitle(answer.Intent)
	selectionCopy := sel
	app.showAnswer(title, answer.Result, sel.Anchor, &selectionCopy, allowApply, generation)
	app.setStatus(traySuccess)
	app.logger.Printf("popup result queued; chars=%d", utf8.RuneCountInString(answer.Result))
}

// Dispatch popup paint, command and close events through the Win32 message loop.
