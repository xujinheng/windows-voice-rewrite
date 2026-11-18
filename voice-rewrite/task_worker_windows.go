//go:build windows

package main

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"time"
	"unicode/utf8"

	"voicerewrite/internal/core/audio"
	"voicerewrite/internal/core/clipboard"
	"voicerewrite/internal/core/delivery"
	"voicerewrite/internal/core/indicator"
	"voicerewrite/internal/core/input"
	"voicerewrite/internal/core/session"
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
	budget := session.StageBudget(session.StageASR, len(r.pcm), 0)
	if !app.epoch.StartStage(generation, session.StageASR, budget) {
		return
	}
	ctx, cancel := context.WithTimeout(app.epoch.ContextFor(generation), budget)
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
	if !app.epoch.FinishStage(generation, session.StageASR) {
		return
	}
	// Both hosts execute the exact same Go effect. Record history only once,
	// before the single native write; the receipt never adds another entry.
	effect := ux.DictationTaskEffect(text, isSameForegroundWindow(expectedWindow))
	if effect.Phase == int(indicator.Error) {
		app.fail(fmt.Errorf("%s", effect.RecordingStatus))
		app.postHandyPhase(indicator.Error, generation)
		return
	}
	for _, entry := range effect.History {
		app.history.Remember(entry.Title, entry.Value)
	}
	if effect.CardText != "" {
		app.showAnswer(effect.CardTitle, effect.CardText, currentCursorPoint(), nil, false, generation)
		app.postHandyPhase(indicator.Phase(effect.Phase), generation)
		return
	}
	if effect.InsertText == "" || !app.epoch.Valid(generation) {
		return
	}
	start = time.Now()
	if !app.epoch.StartStage(generation, session.StageNativeWrite, session.StageBudget(session.StageNativeWrite, 0, 0)) {
		return
	}
	err = insertDictationText(app.Window, expectedWindow, effect.InsertText, func() bool { return app.epoch.Valid(generation) })
	if !app.epoch.FinishStage(generation, session.StageNativeWrite) {
		return
	}
	if !app.epoch.Valid(generation) {
		return
	}
	receipt := delivery.SentUnverified
	if err != nil {
		receipt = delivery.Uncertain // native input might already have been posted
	}
	follow := ux.TaskWriteEffect(delivery.Dictation, receipt, text, ux.DictationWriteUnconfirmed, ux.Present(err))
	if follow.CardText != "" {
		app.fail(err)
		app.showAnswer(follow.CardTitle, follow.CardText, currentCursorPoint(), nil, false, generation)
	} else {
		app.logger.Printf("stage=paste flow=dictation ms=%d chars=%d receipt=sent_unverified", time.Since(start).Milliseconds(), utf8.RuneCountInString(text))
		app.setStatus(traySuccess)
	}
	app.postHandyPhase(indicator.Phase(follow.Phase), generation)
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
	if err := ux.HoldEditorReady(isSameForegroundWindow(expectedWindow)); err != nil {
		rec.Stop()
		go func() { <-rec.result }()
		app.fail(err)
		return
	}
	sel, err := captureSelection(app.Window, app.cfg)
	sel.Anchor = anchor
	defer restoreSelectionClipboardIfOwned(app.Window, sel)
	app.logger.Printf("stage=selection mode=hold ms=%d", time.Since(started).Milliseconds())
	if err == nil {
		err = ux.HoldCapturedWindow(sel.TargetWindow == expectedWindow)
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
	if !app.epoch.StartStage(generation, session.StageSpokenEdit, session.StageBudget(session.StageSpokenEdit, len(r.pcm), 0)) {
		return
	}
	ctx, cancel := context.WithTimeout(app.epoch.ContextFor(generation), session.StageBudget(session.StageSpokenEdit, len(r.pcm), 0))
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
	if !app.epoch.FinishStage(generation, session.StageSpokenEdit) {
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
	route := "preview"
	if action == textedit.Replace || action == textedit.TryReplace {
		route = "replace"
	}
	effect := ux.SelectedTaskEffect(answer.Intent, answer.Result, instruction, route, true)
	app.logger.Printf("route=%s intent=%s native_editable=%t browser_surface=%t", action, answer.Intent, editable, browserSurface)
	if effect.Phase == int(indicator.Error) {
		app.fail(fmt.Errorf("%s", effect.InteractionStatus))
		app.postHandyPhase(indicator.Error, generation)
		return
	}
	for _, entry := range effect.History {
		app.history.Remember(entry.Title, entry.Value)
	}
	if effect.ReplaceText != "" && app.epoch.Valid(generation) {
		app.postHandyPhase(indicator.Phase(effect.Phase), generation)
		startedPaste := time.Now()
		// Native target/snapshot checks remain platform-owned.
		if !app.epoch.StartStage(generation, session.StageNativeWrite, session.StageBudget(session.StageNativeWrite, 0, 0)) {
			return
		}
		e = replaceSelectionWithGuard(app.Window, sel, effect.ReplaceText, app.cfg, action == textedit.TryReplace, func() bool { return app.epoch.Valid(generation) })
		if !app.epoch.FinishStage(generation, session.StageNativeWrite) {
			return
		}
		if !app.epoch.Valid(generation) {
			return
		}
		app.logger.Printf("stage=paste ms=%d", time.Since(startedPaste).Milliseconds())
		receipt := delivery.SentUnverified
		if e != nil {
			receipt = delivery.Uncertain
			app.logger.Printf("auto replacement outcome uncertain: %v", e)
		}
		write := ux.TaskWriteEffect(delivery.ReplaceSelection, receipt, effect.ReplaceText,
			effect.CardTitle, ux.Present(e))
		if write.CardText == "" {
			app.setStatus(traySuccess)
			app.postHandyPhase(indicator.Phase(write.Phase), generation)
			app.logger.Printf("paste_status=unverified intent=%s result_chars=%d", answer.Intent, utf8.RuneCountInString(answer.Result))
			return
		}
		// Never offer a second apply for a possibly delivered edit.
		app.showAnswer(write.CardTitle, write.CardText, sel.Anchor, nil, false, generation)
		app.setStatus(traySuccess)
		app.postHandyPhase(indicator.Phase(write.Phase), generation)
		return
	}
	// Preview-only work never touches the source editor.
	app.showAnswer(effect.CardTitle, effect.CardText, sel.Anchor, nil, false, generation)
	app.setStatus(traySuccess)
	app.postHandyPhase(indicator.Phase(effect.Phase), generation)
}

// Dispatch popup paint, command and close events through the Win32 message loop.
