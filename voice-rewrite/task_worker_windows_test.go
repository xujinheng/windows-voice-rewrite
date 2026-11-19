//go:build windows

package main

import (
	"io"
	"log"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestCanceledRewriteHoldReleasesBusy(t *testing.T) {
	app := &appState{}
	generation := app.epoch.Start()
	app.busy.Store(true) // finish() marks post-record processing busy
	app.epoch.Start()    // dismissing the active pill cancels that task
	rec := &audioRecorder{done: make(chan struct{})}

	processRecordedHold(app, rec, 0, trayPoint{}, generation)

	if app.busy.Load() {
		t.Fatal("canceled hold kept the app busy and blocked the next recording")
	}
	select {
	case <-rec.done:
	default:
		t.Fatal("canceled hold did not stop its recorder")
	}
}

func TestRecordingWatchersPostTaskGeneration(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Use the built-in Win32 class: another test already registers the
	// application's tray window class in this process.
	class, _ := syscall.UTF16PtrFromString("STATIC")
	hwnd, _, err := procCreateWindow.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	runtime.KeepAlive(class)
	if hwnd == 0 {
		t.Fatalf("create message sink: %v", err)
	}
	defer procDestroyWindow.Call(hwnd)

	logger := log.New(io.Discard, "", 0)
	const previousGeneration uint64 = 41
	const activeGeneration uint64 = 42
	rec := &audioRecorder{finished: make(chan struct{})}
	rec.maxReached.Store(true)
	watchRecordingLimit(hwnd, rec, previousGeneration, logger)
	close(rec.finished)

	canceled := make(chan struct{})
	defer close(canceled)
	go watchMouseHoldTimeout(hwnd, uintptr(activeGeneration), canceled, 10*time.Millisecond, logger)

	// The UI must not apply the older recording's queued duration notice to
	// the currently active one. Both watchers tag their own recording epoch.
	want := map[uint32]uintptr{
		WM_RECORD_LIMIT:  uintptr(previousGeneration),
		WM_HOLD_TIMEOUT:  uintptr(activeGeneration),
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(want) != 0 && time.Now().Before(deadline) {
		var msg winMSG
		if got, _, _ := procPeekMessageForTest.Call(uintptr(unsafe.Pointer(&msg)), hwnd, 0, 0, 1); got == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if expected, ok := want[msg.Message]; ok {
			if msg.WParam != expected {
				t.Fatalf("message %#x epoch = %d, want %d", msg.Message, msg.WParam, expected)
			}
			delete(want, msg.Message)
		}
	}
	if len(want) != 0 {
		t.Fatalf("recording watchers did not post expected epoch-tagged notices: %v", want)
	}
}
