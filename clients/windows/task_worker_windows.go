//go:build windows

package main

import (
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"time"
	"unicode/utf8"

	"github.com/xujinheng/windows-voice-rewrite/core/audio"
	"github.com/xujinheng/windows-voice-rewrite/core/indicator"
	"github.com/xujinheng/windows-voice-rewrite/core/ux"
	"github.com/xujinheng/windows-voice-rewrite/core/voice"
)

// A recorder may reach its hardware duration limit before the held key or
// mouse is released. Notify the UI using the same epoch as other hold events;
// the audio worker remains the sole consumer of the buffered PCM result.
func watchRecordingLimit(hwnd uintptr, rec *audioRecorder, generation uint64, logger *log.Logger) {
	go func() {
		<-rec.finished
		if !rec.maxReached.Load() {
			return
		}
		if ok, _, err := procPostMessage.Call(hwnd, WM_RECORD_LIMIT, uintptr(generation), 0); ok == 0 {
			logger.Printf("ERROR: cannot notify UI of recording limit: %v", err)
		}
	}()
}

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
			if down {
				releasedTicks = 0
			} else {
				releasedTicks++
			}
			if releasedTicks >= 2 {
				logger.Printf("hold watcher detected key release; session=%d elapsed=%s", session, time.Since(start).Truncate(time.Millisecond))
				if ok, _, err := procPostMessage.Call(hwnd, WM_HOLD_RELEASE, session, 0); ok == 0 {
					logger.Printf("ERROR: cannot post hold release: %v", err)
				}
				return
			}
		}
	}
}

// A native recording supplies either a caret target (dictation) or selected
// text (rewrite). Both use one worker and the same public voice.Core.Execute.
type recordingTask struct {
	dictation bool
	target    uintptr
	anchor    trayPoint
	selection *selectedText // set by toggle mode; hold mode reads on release
}

func processRecording(app *appState, rec *audioRecorder, task recordingTask, generation uint64) {
	defer app.busy.Store(false)
	defer func() {
		if x := recover(); x != nil {
			app.logger.Printf("voice worker panic: %v\n%s", x, debug.Stack())
			app.fail(fmt.Errorf("录音处理线程发生错误：%v", x))
			app.postHandyPhase(indicator.Error, generation)
		}
	}()
	if !app.core.Snapshot(0, generation).Valid {
		rec.Stop()
		return
	}
	var sel selectedText
	if !task.dictation {
		if task.selection != nil {
			sel = *task.selection
		} else {
			started := time.Now()
			// The physical shortcut must be released before native selection
			// capture; a changed window is not a different edit target.
			if !isSameForegroundWindow(task.target) {
				rec.Stop()
				app.fail(errors.New(ux.HoldWindowChanged))
				return
			}
			var err error
			sel, err = captureSelection(app.Window, app.cfg)
			sel.Anchor = task.anchor
			if err == nil && sel.TargetWindow != task.target {
				err = errors.New(ux.HoldSelectionMoved)
			}
			if err != nil {
				sel.ClipboardBackup.restoreIfOwned(app.Window)
				app.fail(err)
				return
			}
			app.logger.Printf("stage=selection mode=hold ms=%d chars=%d",
				time.Since(started).Milliseconds(), utf8.RuneCountInString(sel.Text))
			app.postHandyPhase(indicator.Processing, generation)
		}
		defer sel.ClipboardBackup.restoreIfOwned(app.Window)
	}

	app.logger.Print("worker: waiting for recording buffer")
	r := <-rec.result
	if !app.core.Snapshot(0, generation).Valid {
		return
	}
	if r.err != nil {
		app.fail(r.err)
		if task.dictation {
			app.postHandyPhase(indicator.Error, generation)
		}
		return
	}
	app.logger.Printf("audio capture complete; %.2f seconds", float64(len(r.pcm))/audio.PCMBytesPerSecond)
	started := time.Now()
	var afterASR func(voice.Event)
	if !task.dictation {
		afterASR = func(event voice.Event) {
			if event.Kind != voice.EventTranscribed {
				return
			}
			if !app.core.Snapshot(0, generation).Valid {
				return
			}
			app.logger.Printf("stage=asr ms=%d", time.Since(started).Milliseconds())
			app.setStatus(trayRewriting)
			app.postHandyPhase(indicator.Rewriting, generation)
			app.logger.Printf("ASR instruction decoded; chars=%d", utf8.RuneCountInString(event.Text))
		}
	}
	answer, err := app.core.Execute(generation, app.cfg, r.pcm, sel.Text, !task.dictation, afterASR)
	if !app.core.Snapshot(0, generation).Valid {
		return
	}
	if err != nil {
		app.fail(err)
		if task.dictation {
			app.postHandyPhase(indicator.Error, generation)
		}
		return
	}

	if task.dictation {
		app.logger.Printf("stage=asr flow=dictation ms=%d", time.Since(started).Milliseconds())
		text := answer.Text
		switch app.core.DecideDelivery(generation, answer, voice.DeliveryFacts{
			TargetCurrent: isSameForegroundWindow(task.target),
		}) {
		case voice.DeliveryDrop:
			return
		case voice.DeliveryPreview:
			app.showAnswer(ux.DictationNotInserted, text, currentCursorPoint(), nil, false, generation)
			app.postHandyPhase(indicator.Fallback, generation)
			return
		}
		started = time.Now()
		err = insertDictationText(app.Window, task.target, text, func() bool { return app.core.Snapshot(0, generation).Valid })
		if err != nil {
			app.fail(err)
			if app.core.DecideDelivery(generation, answer, voice.DeliveryFacts{Failed: true}) == voice.DeliveryPreview {
				app.showAnswer(ux.DictationCopyManually, text, currentCursorPoint(), nil, false, generation)
				app.postHandyPhase(indicator.Fallback, generation)
			}
			return
		}
		app.logger.Printf("stage=paste flow=dictation ms=%d chars=%d status=unverified",
			time.Since(started).Milliseconds(), utf8.RuneCountInString(text))
		app.setStatus(traySuccess)
		app.postHandyPhase(indicator.Success, generation)
		return
	}

	app.logger.Printf("stage=spoken_selection ms=%d intent=%s route=%s",
		time.Since(started).Milliseconds(), answer.Intent, answer.Route)
	delivery := app.core.DecideDelivery(generation, answer, voice.DeliveryFacts{
		TargetCurrent: isSameForegroundWindow(sel.TargetWindow),
		SelectionAvailable: sel.Text != "",
	})
	if delivery == voice.DeliveryDrop {
		return
	}
	if delivery == voice.DeliveryReplace {
		app.postHandyPhase(indicator.Applying, generation)
		started = time.Now()
		// Native editability determines whether to verify the pasted selection.
		err = replaceSelectionWithGuard(app.Window, sel, answer.Text, app.cfg,
			!isSafelyEditableControl(sel.TargetWindow), func() bool { return app.core.Snapshot(0, generation).Valid })
		app.logger.Printf("stage=paste ms=%d", time.Since(started).Milliseconds())
		if err == nil {
			app.setStatus(traySuccess)
			app.postHandyPhase(indicator.Success, generation)
			app.logger.Printf("paste_status=unverified intent=%s result_chars=%d",
				answer.Intent, utf8.RuneCountInString(answer.Text))
			return
		}
		app.logger.Printf("auto replacement failed; preview instead: %v", err)
		delivery = app.core.DecideDelivery(generation, answer, voice.DeliveryFacts{Failed: true})
	}
	if delivery != voice.DeliveryPreview {
		return
	}
	selectionCopy := sel
	app.showAnswer(answer.Title, answer.Text, sel.Anchor,
		&selectionCopy, answer.Route == voice.RouteReplace, generation)
	app.setStatus(traySuccess)
	app.logger.Printf("popup result queued; chars=%d", utf8.RuneCountInString(answer.Text))
}

// Dispatch popup paint, command and close events through the Win32 message loop.
