//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"voicerewrite/internal/core/audio"
	meetingcore "voicerewrite/internal/core/meeting"
	"voicerewrite/internal/core/speech"
)

const (
	WM_MEETING_START   = 0x8000 + 80
	WM_MEETING_STOP    = 0x8000 + 81
	WM_MEETING_REFRESH = 0x8000 + 82
	WM_MEETING_DEMO    = 0x8000 + 83
	WM_MEETING_SUGGEST = 0x8000 + 84
)

var (
	meetingEnumWindows   = user32.NewProc("EnumWindows")
	meetingWindowVisible = user32.NewProc("IsWindowVisible")
	meetingWindowPID     = user32.NewProc("GetWindowThreadProcessId")
	meetingWindowTitle   = user32.NewProc("GetWindowTextW")
	meetingOpenProcess   = kernel32.NewProc("OpenProcess")
	meetingProcessImage  = kernel32.NewProc("QueryFullProcessImageNameW")
)

type meetingSource struct {
	pid         uint32
	name, title string
}

// windowsProcessAudioSource adapts Windows WASAPI process-loopback to the
// platform-neutral PCM frame stream consumed by the meeting pipeline.
type windowsProcessAudioSource struct{ pid uint32 }

var _ audio.FrameSource = windowsProcessAudioSource{}

func (source windowsProcessAudioSource) Stream(ctx context.Context, started func(), push func([]byte)) error {
	return captureMeetingAudio(ctx, source.pid, started, push)
}

func runningMeetingSources() []meetingSource {
	current := uint32(syscall.Getpid())
	seen := map[uint32]bool{}
	var sources []meetingSource
	// The callback is cached by Windows/Go. Pass per-enumeration state as native input.
	state := meetingEnumeration{current: current, seen: seen, sources: &sources}
	meetingEnumWindows.Call(meetingEnumCallback, uintptr(unsafe.Pointer(&state)))
	sort.SliceStable(sources, func(i, j int) bool {
		a, b := sources[i], sources[j]
		if strings.EqualFold(a.name, "Zoom.exe") != strings.EqualFold(b.name, "Zoom.exe") {
			return strings.EqualFold(a.name, "Zoom.exe")
		}
		return strings.ToLower(a.title) < strings.ToLower(b.title)
	})
	return sources
}

type meetingEnumeration struct {
	current uint32
	seen    map[uint32]bool
	sources *[]meetingSource
}

var meetingEnumCallback = syscall.NewCallback(func(hwnd, statePtr uintptr) uintptr {
	state := (*meetingEnumeration)(unsafe.Pointer(statePtr))
	visible, _, _ := meetingWindowVisible.Call(hwnd)
	if visible == 0 {
		return 1
	}
	var pid uint32
	meetingWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == state.current || pid == 0 || state.seen[pid] {
		return 1
	}
	var title [120]uint16
	if n, _, _ := meetingWindowTitle.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title))); n == 0 {
		return 1
	}
	handle, _, _ := meetingOpenProcess.Call(0x1000, 0, uintptr(pid)) // PROCESS_QUERY_LIMITED_INFORMATION
	if handle == 0 {
		return 1
	}
	defer procCloseHandle.Call(handle)
	var image [1024]uint16
	count := uint32(len(image))
	if ok, _, _ := meetingProcessImage.Call(handle, 0, uintptr(unsafe.Pointer(&image[0])), uintptr(unsafe.Pointer(&count))); ok == 0 {
		return 1
	}
	name := syscall.UTF16ToString(image[:count])
	if idx := strings.LastIndexByte(name, '\\'); idx >= 0 {
		name = name[idx+1:]
	}
	state.seen[pid] = true
	*state.sources = append(*state.sources, meetingSource{pid: pid, name: name, title: syscall.UTF16ToString(title[:])})
	return 1
})

func newMeetingSession(app *appState, demo bool) *meetingSession {
	s := newMeetingCore(app.cfg, app.logger.Printf, saveConfig)
	s.demo = demo
	s.onConfig = func(cfg Config) { app.cfg = cfg }
	return s
}

func (s *meetingSession) express(app *appState, rec *audioRecorder, generation uint64, mode string) {
	defer app.busy.Store(false)
	result := <-rec.result
	if !app.epoch.valid(generation) || s.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	ctx, cancel, epoch, serial := s.expressionContextLocked(50 * time.Second)
	s.mu.Unlock()
	stop := context.AfterFunc(app.epoch.contextFor(generation), cancel)
	defer stop()
	defer cancel()

	label := "正在翻译你说的话…"
	if mode == "compose" {
		label = "正在理解表达意图…"
	}
	s.applyExpression(epoch, serial, func() {
		// Keep the previous completed English available to read and copy
		// while the next recognition/model request is running.
		s.view.expression = meetingRetainEnglish(s.view.expression)
		s.view.expressionSource = ""
		s.view.expressionStatus = label
	})
	if result.err != nil {
		s.expressionFailed(app, generation, epoch, serial, "麦克风录音失败", result.err)
		return
	}
	if len(result.pcm) < 3200 {
		s.expressionFailed(app, generation, epoch, serial, "没有录到足够语音", errors.New("请按住快捷键说话后松开"))
		return
	}

	var points string
	cfg := s.configSnapshot()
	err := s.runInference(ctx, meetingcore.PriorityExpress, "正在识别你的中文", func(active context.Context) error {
		var e error
		points, e = speech.Transcribe(active, cfg, result.pcm)
		return e
	})
	if !app.epoch.valid(generation) || ctx.Err() != nil {
		return
	}
	if err != nil {
		s.expressionFailed(app, generation, epoch, serial, "语音识别失败", err)
		return
	}
	points = strings.TrimSpace(points)
	if points == "" {
		s.expressionFailed(app, generation, epoch, serial, "语音识别结果为空", errors.New("请调整麦克风音量后重试"))
		return
	}
	s.applyExpression(epoch, serial, func() {
		s.view.expressionSource = points
		s.view.expressionStatus = label
	})
	var prior []string
	if mode == "compose" {
		s.mu.Lock()
		prior = append([]string(nil), s.prior...)
		s.mu.Unlock()
	}
	app.logger.Printf("meeting expression mode=%s recognized_chars=%d", mode, len([]rune(points)))
	err = s.complete(ctx, meetingcore.PriorityExpress, meetingcore.Query{Mode: mode, Prior: prior, UserPoints: points}, func(line meetingLine) {
		if !app.epoch.valid(generation) || line.kind != "SAY" {
			return
		}
		s.applyExpression(epoch, serial, func() {
			s.view.expression = line
			s.view.expressionStatus = ""
		})
	})
	if !app.epoch.valid(generation) || ctx.Err() != nil || s.ctx.Err() != nil {
		return
	}
	if err != nil {
		s.expressionFailed(app, generation, epoch, serial, "英文生成失败", err)
		return
	}
	app.setStatus(traySuccess)
	app.postHandyPhase(statusAnswer, generation)
}

func (s *meetingSession) expressionFailed(app *appState, generation, epoch, serial uint64, label string, cause error) {
	if !app.epoch.valid(generation) || s.ctx.Err() != nil {
		return
	}
	message := label + "：" + cause.Error()
	s.applyExpression(epoch, serial, func() {
		s.view.expressionStatus = message
		s.view.expression = meetingRetainEnglish(s.view.expression)
	})
	app.fail(fmt.Errorf("%s: %w", label, cause))
	app.postHandyPhase(statusError, generation)
}

func (a *appState) closeMeeting() {
	if a.meeting != nil {
		a.meeting.stop()
		a.meeting = nil
	}
	closeMeetingHUD()
}

func (a *appState) beginMeeting(pid uint32, demo bool) {
	a.closeMeeting()
	s := newMeetingSession(a, demo)
	s.view.status = "正在连接会议音频…"
	s.view.asrMode = s.cfg.MeetingASRMode
	s.view.sourceName = fmt.Sprintf("应用 PID %d", pid)
	for _, source := range runningMeetingSources() {
		if source.pid == pid {
			s.view.sourceName = source.name + " · " + source.title
			break
		}
	}
	if demo {
		s.view = demoMeeting()
	}
	if err := showMeetingHUD(a.Window, s.view); err != nil {
		s.stop()
		a.fail(err)
		return
	}
	a.meeting = s
	activeMeetingHUD.app = a
	procSetTimer.Call(activeMeetingHUD.window, 1, 100, 0)
	if !demo {
		s.start(windowsProcessAudioSource{pid: pid})
	}
}

func (a *appState) refreshMeeting() {
	if a.meeting == nil {
		return
	}
	s := a.meeting
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	s.dirty = false
	err := s.captureErr
	view := s.view
	view.captions = append([]meetingcore.Caption(nil), view.captions...)
	s.mu.Unlock()
	if err != nil {
		a.closeMeeting()
		a.fail(fmt.Errorf("会议采集：%w", err))
		winErr("会议音频采集失败：" + err.Error())
		return
	}
	updateMeetingHUD(view)

}
