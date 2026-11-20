//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/audio"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/indicator"
	meetingcore "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/meeting"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/voice"
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
		return meetingcore.MeetingSourceLess(sources[i].name, sources[i].title, sources[j].name, sources[j].title)
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

// Native Windows owns process selection, microphone and HUD painting only.
// The complete Meeting transaction and its presentation state live in Go Core.
type meetingSession = voice.MeetingSession
type meetingView = voice.MeetingView
type meetingMetrics = voice.MeetingMetrics

func newMeetingSession(app *appState, demo bool) *meetingSession {
	s := voice.NewMeetingSession(app.cfg, app.logger.Printf, saveConfig)
	s.SetDemo(demo)
	s.SetOnConfig(func(cfg Config) { app.cfg = cfg })
	return s
}

func expressMeeting(s *meetingSession, app *appState, rec *audioRecorder, generation uint64, mode string) {
	defer app.busy.Store(false)
	result := <-rec.result
	if !app.core.Valid(generation) || s.Context().Err() != nil {
		return
	}
	err := s.Express(app.core.ContextFor(generation), result.pcm, result.err, mode)
	if !app.core.Valid(generation) || s.Context().Err() != nil || errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		app.fail(err)
		app.postHandyPhase(indicator.Error, generation)
		return
	}
	app.setStatus(traySuccess)
	app.postHandyPhase(indicator.Answer, generation)
}

func (a *appState) closeMeeting() {
	if a.meeting != nil {
		a.meeting.Stop()
		a.meeting = nil
	}
	closeMeetingHUD()
}

func (a *appState) beginMeeting(pid uint32, demo bool) {
	a.closeMeeting()
	s := newMeetingSession(a, demo)
	sourceName := fmt.Sprintf("应用 PID %d", pid)
	for _, source := range runningMeetingSources() {
		if source.pid == pid {
			sourceName = source.name + " · " + source.title
			break
		}
	}
	var demoView *meetingView
	if demo {
		view := demoMeeting()
		demoView = &view
	}
	view := s.Initialize(sourceName, demoView)
	if err := showMeetingHUD(a.Window, view); err != nil {
		s.Stop()
		a.fail(err)
		return
	}
	a.meeting = s
	activeMeetingHUD.app = a
	procSetTimer.Call(activeMeetingHUD.window, 1, 100, 0)
	if !demo {
		s.Start(windowsProcessAudioSource{pid: pid})
	}
}

func (a *appState) refreshMeeting() {
	if a.meeting == nil {
		return
	}
	view, err, dirty := a.meeting.Refresh()
	if !dirty {
		return
	}
	if err != nil {
		a.closeMeeting()
		a.fail(fmt.Errorf("会议采集：%w", err))
		winErr("会议音频采集失败：" + err.Error())
		return
	}
	updateMeetingHUD(view)
}
