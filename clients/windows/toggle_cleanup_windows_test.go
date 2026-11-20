//go:build windows

package main

import (
	"io"
	"log"
	"testing"
)

// A canceled toggle can finish preparing after its epoch was invalidated.
// Its stale ready message must take the same cleanup path as preparation errors.
func TestCanceledToggleCleanupReleasesRecorderAndBusy(t *testing.T) {
	app := &appState{logger: log.New(io.Discard, "", 0)}
	app.busy.Store(true)
	rec := &audioRecorder{done: make(chan struct{})}
	// No clipboard ownership was acquired by this fixture. The real cleanup
	// restores only clipboard formats still owned by the canceled task.
	finishTogglePrepare(app, 0, selectedText{}, rec, true, nil)
	select {
	case <-rec.done:
	default:
		t.Fatal("stale toggle did not stop its recorder")
	}
	if app.busy.Load() {
		t.Fatal("stale toggle kept the app busy and blocked a new recording")
	}
}
