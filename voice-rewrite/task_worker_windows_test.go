//go:build windows

package main

import "testing"

func TestCanceledRewriteHoldReleasesBusy(t *testing.T) {
	app := &appState{}
	generation := app.epoch.Start()
	app.busy.Store(true) // finish() marks post-record processing busy
	app.epoch.Start()     // dismissing the active pill cancels that task
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
