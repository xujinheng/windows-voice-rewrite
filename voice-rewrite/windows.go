//go:build windows

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"voicerewrite/internal/core/history"
	"voicerewrite/internal/core/indicator"
	"voicerewrite/internal/core/input"
	"voicerewrite/internal/core/logfile"
	meetingcore "voicerewrite/internal/core/meeting"
	"voicerewrite/internal/core/session"
	"voicerewrite/internal/core/shortcut"
	"voicerewrite/internal/core/textedit"
	"voicerewrite/internal/core/update"
	"voicerewrite/internal/core/ux"
)

var buildID = "0.29.0-dev" // overwritten by -ldflags -X at build time

var (
	user32                         = syscall.NewLazyDLL("user32.dll")
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	winmm                          = syscall.NewLazyDLL("winmm.dll")
	procRegisterHotKey             = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey           = user32.NewProc("UnregisterHotKey")
	procGetMessage                 = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessage            = user32.NewProc("DispatchMessageW")
	procSetTimer                   = user32.NewProc("SetTimer")
	procKillTimer                  = user32.NewProc("KillTimer")
	procGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	procIsWindow                   = user32.NewProc("IsWindow")
	procSendInput                  = user32.NewProc("SendInput")
	procGetAsyncKeyState           = user32.NewProc("GetAsyncKeyState")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procMessageBox                 = user32.NewProc("MessageBoxW")
	procCreateWindow               = user32.NewProc("CreateWindowExW")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
	procGlobalSize                 = kernel32.NewProc("GlobalSize")
	procCreateMutex                = kernel32.NewProc("CreateMutexW")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procWaveInOpen                 = winmm.NewProc("waveInOpen")
	procWaveInGetNumDevs           = winmm.NewProc("waveInGetNumDevs")
	procWaveInGetDevCaps           = winmm.NewProc("waveInGetDevCapsW")
	procWaveInPrepareHeader        = winmm.NewProc("waveInPrepareHeader")
	procWaveInUnprepareHeader      = winmm.NewProc("waveInUnprepareHeader")
	procWaveInAddBuffer            = winmm.NewProc("waveInAddBuffer")
	procWaveInStart                = winmm.NewProc("waveInStart")
	procWaveInStop                 = winmm.NewProc("waveInStop")
	procWaveInReset                = winmm.NewProc("waveInReset")
	procWaveInClose                = winmm.NewProc("waveInClose")
)

const (
	WM_HOTKEY        = 0x0312
	WM_TIMER         = 0x0113
	WM_HOLD_RELEASE  = 0x8000 + 50
	WM_HOLD_TIMEOUT  = 0x8000 + 51
	WM_RECORD_LIMIT  = 0x8000 + 75
	WM_TOGGLE_READY  = 0x8000 + 56
	WM_SHOW_ANSWER   = 0x8000 + 61
	WM_APPLY_RESULT  = 0x8000 + 74
	WM_STATUS_UPDATE = 0x8000 + 70
	WM_STATUS_CANCEL = 0x8000 + 71
	HOTKEY_RECORD    = 100
	HOTKEY_EXIT      = 101
	HOTKEY_DICTATION = 102
	MOD_ALT          = 0x0001
	MOD_CTRL         = 0x0002
	MOD_SHIFT        = 0x0004
	VK_F9            = 0x78
	VK_CONTROL       = 0x11
	VK_MENU          = 0x12
	VK_SHIFT         = 0x10
	INPUT_KEYBOARD   = 1
	KEYEVENTF_KEYUP  = 0x0002
	CF_UNICODETEXT   = 13
	GMEM_MOVEABLE    = 0x0002
	WAVE_FORMAT_PCM  = 1
	WAVE_MAPPER      = ^uintptr(0)
	WHDR_DONE        = 0x00000001
)

type winMSG struct {
	Hwnd     uintptr
	Message  uint32
	_        uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	X        int32
	Y        int32
	LPrivate uint32
}
type winKEYBDINPUT struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}
type winINPUT struct {
	Type uint32
	_    uint32
	Key  winKEYBDINPUT
	_    [8]byte
}
type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	Size           uint16
}
type waveHdr struct {
	Data          uintptr
	BufferLength  uint32
	BytesRecorded uint32
	User          uintptr
	Flags         uint32
	Loops         uint32
	Next          uintptr
	Reserved      uintptr
}

type sideCapture struct {
	button       uint16
	targetWindow uintptr
	recorder     *audioRecorder
	err          error
}

type togglePrepareResult struct {
	rec        *audioRecorder
	sel        selectedText
	generation uint64
}

type selectedText struct {
	Text            string
	TargetWindow    uintptr
	ClipboardBackup *clipboardSnapshot
	Anchor          trayPoint
}
type appState struct {
	meeting     *meetingSession // owned by the UI thread
	Window      uintptr
	logger      *log.Logger
	cfg         Config
	busy        atomic.Bool
	epoch       session.Epoch
	status      atomic.Int32
	trayMu      sync.Mutex
	tray        *trayIndicator
	popups      chan popupMessage
	lastErrorMu sync.Mutex
	lastError   string
	history     history.Store
	paused      atomic.Bool
	updateBusy  atomic.Bool
	updateReady chan update.Notice
}

// Shell notification APIs and icon lifetime are owned by the Windows UI thread.
// Worker goroutines update only atomic state and post a message to that thread.
// This prevents tray-icon manipulation concurrently with window shutdown.
func (a *appState) setStatus(s trayState) {
	a.status.Store(int32(s))
	a.logger.Printf("status=%s", trayInfo(s).label)
	if ok, _, err := procPostMessage.Call(a.Window, WM_TRAY_UPDATE, 0, 0); ok == 0 {
		a.logger.Printf("failed to queue tray update: %v", err)
	}
}

// Worker threads never mutate native windows. All phase updates are owned by
// the UI message pump and tagged with the recording epoch to discard late events.
func (a *appState) postHandyPhase(phase indicator.Phase, generation uint64) {
	if a.epoch.Valid(generation) {
		procPostMessage.Call(a.Window, WM_STATUS_UPDATE, uintptr(phase), uintptr(generation))
	}
}

func (a *appState) showAnswer(title, body string, anchor trayPoint, selection *selectedText, allowApply bool, generation uint64) {
	if !a.epoch.Valid(generation) {
		return
	}
	msg := popupMessage{title: title, body: body, anchor: anchor, app: a, selection: selection, allowApply: allowApply, generation: generation}
	select {
	case a.popups <- msg:
	default:
		a.logger.Print("popup queue full; dropping result")
		return
	}
	if ok, _, err := procPostMessage.Call(a.Window, WM_SHOW_ANSWER, 0, 0); ok == 0 {
		a.logger.Printf("cannot show popup: %v", err)
	}
}

func winErr(msg string) {
	title, _ := syscall.UTF16PtrFromString("VoiceRewrite - 语音改写")
	content, _ := syscall.UTF16PtrFromString(msg)
	procMessageBox.Call(0, uintptr(unsafe.Pointer(content)), uintptr(unsafe.Pointer(title)), 0x00000030|0x00040000)
	runtime.KeepAlive(content)
	runtime.KeepAlive(title)
}

func (a *appState) fail(e error) {
	if e == nil {
		return
	}
	a.logger.Printf("ERROR: %v", e)
	a.lastErrorMu.Lock()
	a.lastError = ux.Present(e)
	a.lastErrorMu.Unlock()
	a.setStatus(trayError) // nonmodal; available via tray's last-error command
}

var recordVirtualKey uintptr = VK_F9

// A named Windows mutex prevents two app instances from swallowing the same
// mouse side-button. We hold the handle until process exit.
func singleInstanceGuard() (func(), error) {
	name, err := syscall.UTF16PtrFromString("Local\\VoiceRewrite_OneInstance_v1")
	if err != nil {
		return nil, err
	}
	handle, _, callErr := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	runtime.KeepAlive(name)
	if handle == 0 {
		return nil, fmt.Errorf("CreateMutexW: %w", callErr)
	}
	if callErr == syscall.Errno(183) {
		procCloseHandle.Call(handle)
		return nil, errors.New("VoiceRewrite 已在运行，请先从托盘退出旧版本（或在任务管理器结束旧进程）")
	}
	return func() { procCloseHandle.Call(handle) }, nil
}

// Toggle preparation owns native clipboard/recorder cleanup; no Go policy
// needs to model which Win32 handles exist on each failure path.
func finishTogglePrepare(app *appState, hwnd uintptr, sel selectedText, rec *audioRecorder, restoreClipboard bool, err error) {
	if restoreClipboard {
		_ = sel.ClipboardBackup.restoreIfOwned(hwnd)
	}
	if rec != nil {
		rec.Stop()
	}
	app.fail(err)
	app.busy.Store(false)
}

func runApp() {
	if len(os.Args) == 2 && os.Args[1] == "--meeting-demo" {
		runMeetingDemo()
		return
	}
	if os.Getenv("VOICEREWRITE_VISUAL_PREVIEW") == "settings" {
		runSettingsVisualPreview()
		return
	}
	if os.Getenv("VOICEREWRITE_VISUAL_PREVIEW") == "answer" {
		runAnswerVisualPreview()
		return
	}
	if os.Getenv("VOICEREWRITE_VISUAL_PREVIEW") == "recording" {
		runRecordingVisualPreview()
		return
	}
	runtime.LockOSThread() // RegisterHotKey posts to the registering thread's message queue.
	defer runtime.UnlockOSThread()
	releaseInstance, singleErr := singleInstanceGuard()
	if singleErr != nil {
		winErr(singleErr.Error())
		return
	}
	defer releaseInstance()
	logDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "VoiceRewrite")
	if logDir == "VoiceRewrite" {
		logDir = os.TempDir()
	}
	_ = os.MkdirAll(logDir, 0700)
	logPath := filepath.Join(logDir, "VoiceRewrite.log")
	_ = logfile.Rotate(logPath, logfile.MaxBytes, logfile.Backups)
	f, e := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		f = os.Stderr
	} else {
		defer f.Close()
	}
	l := log.New(f, "", log.LstdFlags)
	defer func() {
		if rec := recover(); rec != nil {
			l.Printf("PANIC: %v\n%s", rec, debug.Stack())
			winErr(fmt.Sprint("程序错误: ", rec))
		}
	}()
	_, configBefore := os.Stat(configPath())
	firstRun := os.IsNotExist(configBefore)
	cfg, e := loadConfig()
	if e != nil {
		winErr(e.Error())
		return
	}
	hwnd, e := createTrayCallbackWindow()
	if e != nil {
		winErr("无法初始化 Windows 托盘消息窗口：" + e.Error())
		return
	}
	defer procDestroyWindow.Call(hwnd)
	app := &appState{Window: hwnd, logger: l, cfg: cfg, popups: make(chan popupMessage, 4), updateReady: make(chan update.Notice, 1)}
	defer app.epoch.Shutdown()
	defer app.closeMeeting()
	defer closeActiveAnswer()
	defer closeNativeSettings()
	defer closeHandyStatus()
	activeHandyAppWindow = hwnd
	ready, e := shortcut.ValidateStartup(cfg.RecordMode, cfg.DictationHotkey, cfg.HotkeyRecord, cfg.HotkeyExit)
	if e != nil {
		winErr(e.Error())
		return
	}
	usingMouse := ready.RewriteMouse
	dictationMouse := ready.DictationMouse
	selectedMouseButton := ready.RewriteButton
	dictationButton := ready.DictationButton
	dictationMods, dictationVK := uintptr(ready.DictationMods), uintptr(ready.DictationVK)
	recordMods, recordVK := uintptr(ready.RewriteMods), uintptr(ready.RewriteVK)
	exitMods, exitVK := uintptr(ready.ExitMods), uintptr(ready.ExitVK)
	if !usingMouse {
		recordVirtualKey = recordVK
		if rv, _, _ := procRegisterHotKey.Call(0, HOTKEY_RECORD, recordMods, recordVK); rv == 0 {
			winErr("Cannot register record hotkey: " + cfg.HotkeyRecord + " (already in use?)")
			return
		}
		defer procUnregisterHotKey.Call(0, HOTKEY_RECORD)
	}
	if !dictationMouse {
		if rv, _, _ := procRegisterHotKey.Call(0, HOTKEY_DICTATION, dictationMods, dictationVK); rv == 0 {
			winErr("无法注册听写快捷键：" + cfg.DictationHotkey + "（可能已被其他程序占用）")
			return
		}
		defer procUnregisterHotKey.Call(0, HOTKEY_DICTATION)
	}
	if rv, _, _ := procRegisterHotKey.Call(0, HOTKEY_EXIT, exitMods, exitVK); rv == 0 {
		winErr("Cannot register exit hotkey: " + cfg.HotkeyExit)
		return
	}
	defer procUnregisterHotKey.Call(0, HOTKEY_EXIT)
	trayIcon, err := newTrayIndicator(hwnd, &app.paused, &app.history)
	if err != nil {
		winErr("无法创建系统托盘图标: " + err.Error())
		return
	}
	app.tray = trayIcon
	defer func() {
		app.trayMu.Lock()
		trayIcon.close()
		app.tray = nil
		app.trayMu.Unlock()
	}()
	l.Printf("started; build=%s mode=%s hotkey=%s dictation=%s", buildID, cfg.RecordMode, cfg.HotkeyRecord, cfg.DictationHotkey)
	if firstRun {
		l.Print("first start: opening setup and service checks")
		if err := showNativeSettings(app); err != nil {
			l.Printf("first start settings: %v", err)
		}
	}
	// The tray icon is the UI. Do not steal focus with an instructional MessageBox.
	// Install the hook on its own locked OS thread; the UI remains independent.
	{
		// Even keyboard-only bindings need the hook while Settings captures
		// Mouse4/Mouse5. Unassigned buttons still pass through untouched.
		mouseHook, hookErr := installSideMouseHook(hwnd, selectedMouseButton, dictationButton, &app.paused)
		if hookErr != nil {
			winErr(hookErr.Error())
			return
		}
		defer mouseHook.close()
		l.Printf("mouse hook installed: rewrite=%d dictation=%d", selectedMouseButton, dictationButton)
	}
	var active *audioRecorder
	var pendingSide *sideCapture
	// Short clicks never create a task or send their captured audio to ASR.
	discardPendingSide := func() {
		if pendingSide != nil && pendingSide.recorder != nil {
			rec := pendingSide.recorder
			rec.Stop()
		}
		pendingSide = nil
		if activeHandyStatus != nil && activeHandyStatus.pending {
			closeHandyStatus()
		}
	}
	defer discardPendingSide()
	var selected selectedText
	toggleResult := make(chan togglePrepareResult, 1)
	var holdTargetWindow uintptr
	var holdButton uint16
	var holdIsDictation bool
	var holdMeeting *meetingSession
	var activeGeneration uint64
	var holdSession uintptr
	var recordingSession uintptr
	var holdWatch chan struct{}
	// The native recorder can reach its duration limit before a held key or
	// mouse button is released. Observe recorder completion without consuming
	// its PCM result (the worker is its sole reader).
	watchRecordingLimit := func(rec *audioRecorder) {
		recordingSession++
		session := recordingSession
		go func() {
			<-rec.finished
			if !rec.maxReached.Load() {
				return
			}
			if ok, _, err := procPostMessage.Call(hwnd, WM_RECORD_LIMIT, session, 0); ok == 0 {
				l.Printf("ERROR: cannot notify UI of recording limit: %v", err)
			}
		}()
	}
	stopHoldWatch := func() {
		if holdWatch != nil {
			close(holdWatch)
			holdWatch = nil
		}
	}
	defer stopHoldWatch()
	finish := func() {
		if active == nil {
			return
		}
		if cfg.RecordMode == "hold" {
			stopHoldWatch()
		}
		rec := active
		active = nil
		rec.Stop()
		app.busy.Store(true) // no second operation until the worker is finished
		app.setStatus(trayTranscribing)
		l.Print("recording stop requested; selection/model processing moved to worker")
		kind := input.FinishRecording(cfg.RecordMode == "hold", holdIsDictation, holdMeeting != nil)
		if kind == input.FinishDictation || kind == input.FinishMeeting {
			app.postHandyPhase(indicator.Processing, activeGeneration)
		} else if kind == input.FinishSpokenRewrite {
			app.postHandyPhase(indicator.Selection, activeGeneration)
		}
		switch kind {
		case input.FinishMeeting:
			if holdMeeting.runtime.Context().Err() != nil {
				// The single buffered recorder result needs no drain worker.
				app.busy.Store(false)
				app.setStatus(trayIdle)
				closeHandyStatus()
			} else {
				go holdMeeting.express(app, rec, activeGeneration, meetingcore.VoiceMode(holdIsDictation))
			}
			holdMeeting = nil
		case input.FinishDictation:
			go processDictationHold(app, rec, holdTargetWindow, activeGeneration)
		case input.FinishSpokenRewrite:
			go processRecordedHold(app, rec, holdTargetWindow, currentCursorPoint(), activeGeneration)
		default:
			sel := selected
			sel.Anchor = currentCursorPoint()
			go processRecorded(app, rec, sel, activeGeneration)
		}
	}

	// A microphone stream may already be capturing the first 250ms before
	// a side-button hold becomes a voice operation.
	activateHold := func(rec *audioRecorder, dictation bool, button uint16, target uintptr) {
		holdTargetWindow = target
		holdButton = button
		holdIsDictation = dictation
		holdMeeting = nil
		if app.meeting != nil && !app.meeting.demo {
			holdMeeting = app.meeting
		}
		activeGeneration = app.epoch.Start()
		active = rec
		watchRecordingLimit(rec)
		app.setStatus(trayRecording)
		holdSession++
		holdWatch = make(chan struct{})
		watchLimit := time.Duration(cfg.MaxRecordSeconds+2) * time.Second
		if button != 0 {
			go watchMouseHoldTimeout(hwnd, holdSession, holdWatch, watchLimit, l)
		} else {
			vk := recordVK
			if dictation {
				vk = dictationVK
			}
			mods := recordMods
			if dictation {
				mods = dictationMods
			}
			go watchHoldRelease(hwnd, vk, mods, holdSession, holdWatch, watchLimit, l)
		}
		statusMode := input.HoldModeLabel(dictation, holdMeeting != nil)
		// Promote the immediate mouse-down preview without recreating its HWND.
		// Keyboard hold has no preview, so the promote call stays on the side-button path.
		promoted := false
		if button != 0 {
			promoted = promotePendingHandyStatus(rec, statusMode, activeGeneration)
		}
		if button == 0 || !promoted {
			showHandyStatus(rec, statusMode, indicator.Recording, activeGeneration)
		}
		// The native status bar is the start feedback. A synchronous Beep
		// blocks this Win32 message loop and makes the shortcut feel sluggish.
		l.Printf("recording active, flow=%s button=%d", map[bool]string{true: "dictation", false: "rewrite"}[dictation], button)
	}

	startHold := func(dictation bool, button uint16) {
		if app.paused.Load() || app.busy.Load() || active != nil || pendingSide != nil ||
			uxActiveSettings != nil && isSameForegroundWindow(uxActiveSettings.window) {
			return
		}
		win, _, _ := procGetForegroundWindow.Call()
		if win == 0 {
			app.fail(errors.New(input.NoTargetWindow))
			return
		}
		rec, err := startRecorder(cfg.MaxRecordSeconds, cfg.AudioDeviceID)
		if err != nil {
			app.fail(err)
			return
		}
		activateHold(rec, dictation, button, win)
	}

	armSide := func(button uint16) {
		if app.paused.Load() || app.busy.Load() || active != nil || pendingSide != nil ||
			input.SideButtonFlow(button, selectedMouseButton, dictationButton) == "ignore" {
			return
		}
		if uxActiveSettings != nil && isSameForegroundWindow(uxActiveSettings.window) {
			return
		}
		win, _, _ := procGetForegroundWindow.Call()
		if win == 0 {
			return
		}
		// Paint the provisional status immediately. Recording starts at
		// physical mouse-down, but only a 250ms hold activates voice mode.
		// A short click closes this preview and replays browser navigation.
		mode := input.HoldModeLabel(input.SideButtonFlow(button, selectedMouseButton, dictationButton) == "dictation", false)
		showPendingHandyStatus(mode)
		rec, err := startRecorder(cfg.MaxRecordSeconds, cfg.AudioDeviceID)
		pendingSide = &sideCapture{button: button, targetWindow: win, recorder: rec, err: err}
	}

	confirmSide := func(button uint16) {
		if pendingSide == nil || pendingSide.button != button {
			return
		}
		held := pendingSide
		if app.paused.Load() || app.busy.Load() || active != nil ||
			held.targetWindow == 0 || !isSameForegroundWindow(held.targetWindow) {
			discardPendingSide()
			return
		}
		pendingSide = nil
		if held.err != nil {
			if activeHandyStatus != nil && activeHandyStatus.pending {
				closeHandyStatus()
			}
			app.fail(held.err)
			return
		}
		// Leave the provisional pill alive: activateHold promotes it in-place.
		activateHold(held.recorder, input.SideButtonFlow(button, selectedMouseButton, dictationButton) == "dictation", button, held.targetWindow)
	}

	for {
		var msg winMSG
		rv, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(rv) <= 0 {
			break
		}
		switch msg.Message {
		case WM_MEETING_START, WM_MEETING_SUGGEST, WM_MEETING_DEMO, WM_MEETING_STOP, WM_MEETING_REFRESH:
			message := meetingcore.PostedRefresh
			switch msg.Message {
			case WM_MEETING_START:
				message = meetingcore.PostedStart
			case WM_MEETING_SUGGEST:
				message = meetingcore.PostedSuggest
			case WM_MEETING_DEMO:
				message = meetingcore.PostedDemo
			case WM_MEETING_STOP:
				message = meetingcore.PostedStop
			}
			switch meetingcore.PostedMeetingEffect(message, app.paused.Load(), app.meeting != nil, app.meeting != nil && app.meeting.demo) {
			case meetingcore.PostedBegin:
				app.beginMeeting(uint32(msg.WParam), false)
			case meetingcore.PostedBeginDemo:
				app.beginMeeting(0, true)
			case meetingcore.PostedReplies:
				app.meeting.requestReplies()
			case meetingcore.PostedStopMeeting:
				app.closeMeeting()
				if holdMeeting != nil {
					closeHandyStatus()
				}
			case meetingcore.PostedRefreshMeeting:
				app.refreshMeeting()
			}
			continue
		}
		if msg.Message == WM_STATUS_UPDATE {
			hasPill := activeHandyStatus != nil
			if indicator.PhaseUpdateApplies(app.epoch.Valid(uint64(msg.LParam)), hasPill, hasPill && activeHandyStatus.owner == uint64(msg.LParam)) {
				updateHandyStatus(indicator.Phase(msg.WParam))
			}
			continue
		}
		if msg.Message == WM_STATUS_CANCEL {
			status := activeHandyStatus
			switch indicator.CancelChoice(status != nil && status.pending && msg.WParam == 0, status != nil && app.epoch.Valid(uint64(msg.WParam)) && status.owner == uint64(msg.WParam), status != nil && indicator.Terminal(status.phase)) {
			case indicator.CancelPending:
				discardPendingSide()
			case indicator.CancelDismiss:
				closeHandyStatus() // close a completed status, not the answer card
			case indicator.CancelWork:
				app.epoch.Start() // cancel either Mouse4 or Mouse5 before paste
				if active != nil {
					stopHoldWatch()
					rec := active
					active = nil
					rec.Stop()
				}
				closeHandyStatus()
				app.setStatus(trayIdle)
			}
			continue
		}
		if msg.Message == WM_APPLY_RESULT {
			if activeAnswer != nil && activeAnswer.window != 0 && msg.WParam == activeAnswer.window {
				popup := activeAnswer
				if popup.applyResult != nil {
					select {
					case err := <-popup.applyResult:
						if err == nil {
							l.Print("popup apply: selection replaced")
							closeActiveAnswer()
						} else {
							l.Printf("popup apply failed: %v", err)
							popup.setNote(ux.ApplyUnsafe)
						}
					default:
					}
				}
			}
			continue
		}
		if msg.Message == WM_SHOW_ANSWER {
			select {
			case answer := <-app.popups:
				if !app.epoch.Valid(answer.generation) {
					l.Print("discarded stale popup result")
					continue
				}
				if err := showAnswerPopup(answer); err != nil {
					l.Printf("popup window: %v", err)
					app.fail(err)
				} else {
					app.postHandyPhase(indicator.Answer, answer.generation)
					l.Printf("popup native window displayed; chars=%d", utf8.RuneCountInString(answer.body))
				}
			default:
			}
			continue
		}
		if msg.Message == WM_TOGGLE_READY {
			result := <-toggleResult
			if !app.epoch.Valid(result.generation) {
				result.rec.Stop()
				continue
			}
			activeGeneration = result.generation
			active = result.rec
			watchRecordingLimit(active)
			selected = result.sel
			app.busy.Store(false)
			app.setStatus(trayRecording)
			l.Printf("recording started, mode=toggle, selected characters=%d", utf8.RuneCountInString(selected.Text))
			continue
		}
		if msg.Message == WM_TRAY_UPDATE {
			state := trayState(app.status.Load())
			if state == trayError && activeHandyStatus != nil && !indicator.Terminal(activeHandyStatus.phase) && app.epoch.Valid(activeHandyStatus.owner) {
				updateHandyStatus(indicator.Error)
			}
			// Always run Shell_NotifyIconW on the thread that owns the tray window.
			if err := trayIcon.notify(state, false); err != nil {
				l.Printf("tray state update failed: %v", err)
			}
			procKillTimer.Call(0, TRAY_RESET_TIMER_ID)
			if state == traySuccess || state == trayError {
				procSetTimer.Call(0, TRAY_RESET_TIMER_ID, uintptr(ux.TrayResetAfter/time.Millisecond), 0)
			}
			continue
		}
		if msg.Message == WM_TIMER && msg.WParam == TRAY_RESET_TIMER_ID {
			procKillTimer.Call(0, TRAY_RESET_TIMER_ID)
			state := trayState(app.status.Load())
			switch ux.TrayAfterTimer(state == traySuccess, state == trayError, app.paused.Load()) {
			case ux.TrayRestPaused:
				app.setStatus(trayPaused)
			case ux.TrayRestIdle:
				app.setStatus(trayIdle)
			}
			continue
		}
		if msg.Message == WM_TRAY_EVENT {
			event := uint32(msg.LParam & 0xffff)
			if trayMenuRequested(event) {
				l.Printf("tray: context menu requested event=0x%04x", event)
				command, menuErr := trayIcon.contextMenu(app.meeting != nil)
				if menuErr != nil {
					l.Printf("tray: menu failed: %v", menuErr)
				} else {
					l.Printf("tray: menu dismissed command=%d", command)
				}
			}
			continue
		}
		if msg.Message == WM_TRAY_CHECK_UPDATE {
			if !app.updateBusy.CompareAndSwap(false, true) {
				winErr(update.AlreadyChecking)
				continue
			}
			go func() {
				notice := update.Check(buildID)
				select {
				case app.updateReady <- notice:
					procPostMessage.Call(hwnd, WM_TRAY_UPDATE_READY, 0, 0)
				default:
					app.updateBusy.Store(false)
				}
			}()
			continue
		}
		if msg.Message == WM_TRAY_UPDATE_READY {
			app.updateBusy.Store(false)
			select {
			case notice := <-app.updateReady:
				if !notice.AsksToOpen() {
					winErr(notice.Summary(buildID))
				} else if askViewUpdate(notice.Latest) {
					if err := shellOpenLocalFile(update.ReleasesPage); err != nil {
						winErr(update.PageOpenFailed + err.Error())
					}
				}
			default:
			}
			continue
		}
		if msg.Message == WM_TRAY_CONFIG {
			if err := showNativeSettings(app); err != nil {
				app.fail(err)
			}
			continue
		}
		if msg.Message == WM_TRAY_CLOSE_POPUP {
			closeActiveAnswer()
			l.Print("popup closed through tray menu")
			continue
		}
		if msg.Message == WM_TRAY_TOGGLE_PAUSE {
			switch ux.ChoosePause(app.busy.Load() || active != nil, app.paused.Load()) {
			case ux.PauseRefused:
				winErr(ux.PauseWhileBusy)
			case ux.PauseOn:
				app.paused.Store(true)
				app.closeMeeting()
				discardPendingSide()
				app.setStatus(trayPaused)
			case ux.PauseOff:
				app.paused.Store(false)
				app.setStatus(trayIdle)
			}
			continue
		}
		if msg.Message == WM_TRAY_LAST_RESULT {
			recent := app.history.Snapshot()
			if len(recent) == 0 {
				winErr(history.EmptySession)
			} else {
				app.showAnswer(ux.HistoryRestoreTitle(recent[0].Title), recent[0].Text, currentCursorPoint(), nil, false, app.epoch.Current())
			}
			continue
		}
		if msg.Message == WM_TRAY_HISTORY_ITEM {
			if entry, ok := app.history.Get(uint64(msg.WParam)); ok {
				app.showAnswer(ux.HistoryRestoreTitle(entry.Title), entry.Text, currentCursorPoint(), nil, false, app.epoch.Current())
			} else {
				winErr(history.Cleared)
			}
			continue
		}
		if msg.Message == WM_TRAY_CLEAR_HISTORY {
			app.history.Clear()
			closeActiveAnswer()
			continue
		}
		if msg.Message == WM_TRAY_LAST_ERROR {
			app.lastErrorMu.Lock()
			latest := app.lastError
			app.lastErrorMu.Unlock()
			winErr(history.ShownError(latest))
			continue
		}
		if msg.Message == WM_TRAY_LOG {
			if err := shellOpenLocalFile(filepath.Join(logDir, "VoiceRewrite.log")); err != nil {
				app.fail(err)
			}
			continue
		}
		if trayIcon.taskbarCreated != 0 && msg.Message == trayIcon.taskbarCreated {
			if err := trayIcon.notify(trayState(app.status.Load()), true); err != nil {
				l.Printf("tray re-add failed: %v", err)
			}
			continue
		}
		// Settings must receive registered hotkeys (including Exit) before the
		// normal exit command. Otherwise recording a conflicting chord quits
		// the app. Once settings loses focus, global hotkeys work normally.
		if msg.Message == WM_HOTKEY && uxActiveSettings != nil &&
			uxActiveSettings.capturingShortcut != 0 && isSameForegroundWindow(uxActiveSettings.window) {
			vk := uint32((msg.LParam >> 16) & 0xffff)
			mods := uint32(msg.LParam & 0x0007)
			uxActiveSettings.acceptShortcutKey(vk, mods)
			continue
		}
		if msg.Message == WM_TRAY_EXIT || (msg.Message == WM_HOTKEY && msg.WParam == HOTKEY_EXIT) {
			discardPendingSide()
			closeHandyStatus()
			if active != nil {
				stopHoldWatch()
				active.Stop()
			}
			l.Print("stopped via exit action")
			return
		}
		if msg.Message == WM_HOLD_RELEASE {
			if cfg.RecordMode == "hold" && active != nil && msg.WParam == holdSession {
				l.Print("hold key released; completing recording")
				finish()
			}
			continue
		}
		if msg.Message == WM_RECORD_LIMIT {
			if active != nil && msg.WParam == recordingSession {
				l.Print("recording reached configured duration; submitting captured audio")
				finish()
			}
			continue
		}
		if msg.Message == WM_HOLD_TIMEOUT {
			if cfg.RecordMode == "hold" && active != nil && msg.WParam == holdSession {
				// The safety watcher is not proof that the mouse driver lost
				// a release. Preserve the captured speech instead of discarding it.
				l.Print("hold safety deadline reached; submitting available audio")
				finish()
			}
			continue
		}
		if msg.Message == WM_SETTINGS_CAPTURE_MOUSE {
			if p := uxActiveSettings; p != nil {
				if name := input.SettingsMouseCapture(p.capturingShortcut, msg.LParam, msg.WParam); name != "" {
					p.acceptShortcut(name)
				}
			}
			continue
		}
		if msg.Message == WM_SIDE_ARM {
			armSide(uint16(msg.WParam))
			continue
		}
		if msg.Message == WM_SIDE_CANCEL {
			if pendingSide != nil && pendingSide.button == uint16(msg.WParam) {
				discardPendingSide()
			}
			continue
		}
		if msg.Message == WM_SIDE_UP {
			if cfg.RecordMode == "hold" && active != nil && uint16(msg.WParam) == holdButton {
				l.Printf("mouse button %d released; completing recording", holdButton)
				finish()
			}
			continue
		}
		if msg.Message == WM_SIDE_DOWN {
			confirmSide(uint16(msg.WParam))
			continue
		}
		if msg.Message != WM_HOTKEY {
			// All popup paint, close, mouse, keyboard and child WM_COMMAND
			// messages MUST be dispatched to their registered WndProc.
			// The v5 implementation silently discarded them, leaving blank,
			// impossible-to-close windows.
			dispatchNativeMessage(&msg)
			continue
		}
		switch msg.WParam {
		case HOTKEY_DICTATION:
			if !app.paused.Load() {
				startHold(true, 0)
			}
			continue
		case HOTKEY_RECORD:
			switch input.RewriteHotkeyAction(app.paused.Load(), app.busy.Load(), active != nil, cfg.RecordMode == "hold") {
			case input.RewriteFinishToggle:
				finish()
				continue
			case input.RewriteStartHold:
				// Hold mode records immediately and does NOT touch the clipboard
				// while the hotkey is physically held. Capture is performed on release.
				startHold(false, selectedMouseButton)
				continue
			case input.RewriteIgnore:
				continue
			}
			// Legacy toggle mode also prepares its selection on a worker.
			generation := app.epoch.Start()
			app.busy.Store(true)
			app.setStatus(trayTranscribing)
			go func() {
				if !waitKeysReleased(2500 * time.Millisecond) {
					finishTogglePrepare(app, hwnd, selectedText{}, nil, false, errors.New(ux.DictationReleaseKeys))
					return
				}
				begin := time.Now()
				sel, err := captureSelection(hwnd, cfg)
				l.Printf("stage=selection mode=toggle ms=%d", time.Since(begin).Milliseconds())
				if err == nil {
					err = textedit.SelectedLengthError(cfg.MaxSelectedRunes, sel.Text)
				}
				if err != nil {
					finishTogglePrepare(app, hwnd, sel, nil, true, err)
					return
				}
				rec, err := startRecorder(cfg.MaxRecordSeconds, cfg.AudioDeviceID)
				if err != nil {
					finishTogglePrepare(app, hwnd, sel, nil, true, err)
					return
				}
				if !app.epoch.Valid(generation) {
					finishTogglePrepare(app, hwnd, sel, rec, true, nil)
					return
				}
				toggleResult <- togglePrepareResult{rec: rec, sel: sel, generation: generation}
				if ok, _, _ := procPostMessage.Call(hwnd, WM_TOGGLE_READY, 0, 0); ok == 0 {
					// The UI never received the ready notification. Withdraw the
					// buffered result so a later toggle cannot block on a full channel.
					<-toggleResult
					finishTogglePrepare(app, hwnd, sel, rec, false, errors.New(input.ToggleUINotifyFailed))
				}
			}()
		}
	}
}

// watchHoldRelease polls physical key state on its own goroutine instead of relying
// on WM_TIMER messages sharing the GUI message queue. The main UI thread owns all
// state transitions, after a WM_APP message is posted to its hidden window.
// The deadline is a second defense against indefinitely stuck red tray status.

var procIsDialogMessage = user32.NewProc("IsDialogMessageW")

func dispatchNativeMessage(msg *winMSG) {
	if uxActiveSettings != nil && uxActiveSettings.window != 0 {
		if uxSettingsKeyMessage(*msg) {
			return
		}
		if handled, _, _ := procIsDialogMessage.Call(uxActiveSettings.window, uintptr(unsafe.Pointer(msg))); handled != 0 {
			return
		}
	}
	if shouldClosePopupForKey(*msg) {
		closeActiveAnswer()
		return
	}
	// The result is a regular top-level HWND, not a dialog: explicitly
	// translate Tab/Shift+Tab and button activation for its native controls.
	if activeMeetingHUD != nil && activeMeetingHUD.window != 0 {
		if handled, _, _ := procIsDialogMessage.Call(activeMeetingHUD.window, uintptr(unsafe.Pointer(msg))); handled != 0 {
			return
		}
	}
	if activeAnswer != nil && activeAnswer.window != 0 {
		if handled, _, _ := procIsDialogMessage.Call(activeAnswer.window, uintptr(unsafe.Pointer(msg))); handled != 0 {
			return
		}
	}
	procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
	procDispatchMessage.Call(uintptr(unsafe.Pointer(msg)))
}
