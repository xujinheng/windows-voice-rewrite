package main

import (
	"encoding/base64"
	"errors"
	"testing"

	"voicerewrite/internal/core/audio"
	meetingcore "voicerewrite/internal/core/meeting"
)

func TestBridgeMeetingSourceAndBackpressureTrace(t *testing.T) {
	m := &bridgeMeeting{
		runtime: meetingcore.NewSessionRuntime(),
		source:  &meetingPacketSource{packets: make(chan []byte, 1)},
	}
	defer m.runtime.Stop()
	epoch, err := m.runtime.CaptureRoute(meetingcore.CaptureStart, "application-A", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.runtime.CaptureRoute(meetingcore.CaptureStart, "application-A", 0); !errors.Is(err, meetingcore.ErrCaptureActive) {
		t.Fatalf("duplicate start admitted: %v", err)
	}
	frame := make([]byte, audio.FrameBytes)
	if err := m.feed(frame, epoch); err != nil {
		t.Fatal(err)
	}
	if err := m.feed(frame, epoch); !errors.Is(err, errMeetingBusy) {
		t.Fatalf("full packet queue not bounded: %v", err)
	}
	m.noteTransportDrops(3, epoch)
	if m.dropped != 4 || m.status != meetingcore.DroppedWork {
		t.Fatalf("native + Go drops not reported: %d / %s", m.dropped, m.status)
	}
	if err := m.switchSource("application-B", epoch); err != nil {
		t.Fatal(err)
	}
	_, next, _ := m.runtime.Snapshot()
	if next <= epoch || len(m.source.packets) != 0 || m.runtime.CaptureSource() != "application-B" {
		t.Fatalf("source switch leaked old packets: epoch=%d retained=%d", next, len(m.source.packets))
	}
	if err := m.feed(frame, epoch); !errors.Is(err, errMeetingStaleEpoch) {
		t.Fatalf("stale source sent PCM: %v", err)
	}
	m.noteTransportDrops(15, epoch)
	if m.dropped != 4 {
		t.Fatalf("old transport drop attributed to new source: %d", m.dropped)
	}
	if err := m.feed(frame, next); err != nil {
		t.Fatal(err)
	}
	if err := m.sourceLost(epoch); !errors.Is(err, meetingcore.ErrCaptureEpoch) {
		t.Fatalf("late failure canceled new source: %v", err)
	}
	if err := m.sourceLost(next); err != nil {
		t.Fatal(err)
	}
	if len(m.source.packets) != 0 {
		t.Fatal("source loss retained queued PCM")
	}
	if err := m.feed(frame, next); !errors.Is(err, errMeetingPaused) {
		t.Fatalf("lost source still accepts PCM: %v", err)
	}
	if err := m.setPaused(false); !errors.Is(err, meetingcore.ErrCaptureSource) {
		t.Fatalf("source loss resumed without new source: %v", err)
	}
	_, lostEpoch, _ := m.runtime.Snapshot()
	if err := m.switchSource("application-C", lostEpoch); err != nil {
		t.Fatal(err)
	}
	if err := m.setPaused(false); err != nil {
		t.Fatal(err)
	}
	_, current, paused := m.runtime.Snapshot()
	if paused || current <= lostEpoch || !m.runtime.Active(current) {
		t.Fatalf("new source did not resume: epoch=%d paused=%t", current, paused)
	}
}

func TestBridgeCaptureRouteMatchesDirectGoHostTrace(t *testing.T) {
	// The Windows host invokes these same runtime transitions directly;
	// the macOS host invokes them through meeting_* on its Go bridge.
	direct := meetingcore.NewSessionRuntime()
	m := &bridgeMeeting{
		runtime: meetingcore.NewSessionRuntime(),
		source:  &meetingPacketSource{packets: make(chan []byte, 2)},
	}
	defer direct.Stop()
	defer m.runtime.Stop()
	events := []struct {
		event  meetingcore.CaptureEvent
		source string
	}{
		{meetingcore.CaptureStart, "system"},
		{meetingcore.CapturePause, ""},
		{meetingcore.CaptureResume, ""},
		{meetingcore.CaptureSwitch, "application"},
		{meetingcore.CaptureLost, ""},
		{meetingcore.CaptureSwitch, "system"},
		{meetingcore.CaptureResume, ""},
		{meetingcore.CaptureStop, ""},
	}
	for _, e := range events {
		_, directEpoch, _ := direct.Snapshot()
		_, bridgeEpoch, _ := m.runtime.Snapshot()
		a, errA := direct.CaptureRoute(e.event, e.source, directEpoch)
		b, errB := m.runtime.CaptureRoute(e.event, e.source, bridgeEpoch)
		if (errA == nil) != (errB == nil) || a != b {
			t.Fatalf("%s: Windows Go=(%d,%v), bridge Go=(%d,%v)", e.event, a, errA, b, errB)
		}
		_, _, pauseA := direct.Snapshot()
		_, _, pauseB := m.runtime.Snapshot()
		if pauseA != pauseB || direct.Active(a) != m.runtime.Active(b) {
			t.Fatalf("%s: host trace diverged, paused=%v/%v", e.event, pauseA, pauseB)
		}
	}
}

func TestMeetingWireSourceSwitchLostResumeAndStop(t *testing.T) {
	h := startBridge(t, defaultOptions())
	cfg := configFor("http://127.0.0.1:8080")
	h.send(map[string]any{"v": 1, "id": "start-wire", "op": "meeting_start",
		"config": cfg, "source_id": "system"})
	initial := h.receive()
	if !initial.OK {
		t.Fatalf("meeting_start failed: %#v", initial)
	}
	start := object(t, initial.Result)
	if start["feed_window"] != float64(meetingcore.PacketWindow) {
		t.Fatalf("bridge did not advertise Go packet credits: %#v", start)
	}
	id := start["session_id"].(string)
	epoch := start["audio_epoch"].(float64)
	h.send(map[string]any{"v": 1, "id": "switch-wire", "op": "meeting_switch",
		"session_id": id, "source_id": "app-A", "audio_epoch": epoch})
	switched := h.receive()
	if !switched.OK || object(t, switched.Result)["epoch"].(float64) <= epoch {
		t.Fatalf("meeting_switch failed: %#v", switched)
	}
	next := object(t, switched.Result)["epoch"].(float64)
	h.send(map[string]any{"v": 1, "id": "stale-wire", "op": "meeting_feed",
		"session_id": id, "audio_epoch": epoch,
		"pcm_base64": base64.StdEncoding.EncodeToString(make([]byte, audio.FrameBytes))})
	stale := h.receive()
	if !stale.OK || object(t, stale.Result)["stale_epoch"] != true {
		t.Fatalf("old capture feed accepted after switch: %#v", stale)
	}
	h.send(map[string]any{"v": 1, "id": "lost-wire", "op": "meeting_source_lost",
		"session_id": id, "audio_epoch": next})
	lost := h.receive()
	if !lost.OK || object(t, lost.Result)["capture_paused"] != true {
		t.Fatalf("source loss did not pause: %#v", lost)
	}
	h.send(map[string]any{"v": 1, "id": "resume-lost-wire", "op": "meeting_resume", "session_id": id})
	if got := h.receive(); got.OK || got.Error == nil {
		t.Fatalf("source loss automatically resumed: %#v", got)
	}
	lostEpoch := object(t, lost.Result)["epoch"].(float64)
	h.send(map[string]any{"v": 1, "id": "new-source-wire", "op": "meeting_switch",
		"session_id": id, "source_id": "app-B", "audio_epoch": lostEpoch})
	newSource := h.receive()
	if !newSource.OK {
		t.Fatalf("new source rejected: %#v", newSource)
	}
	h.send(map[string]any{"v": 1, "id": "resume-wire", "op": "meeting_resume", "session_id": id})
	resumed := h.receive()
	if !resumed.OK || object(t, resumed.Result)["capture_paused"] != false {
		t.Fatalf("new source did not resume: %#v", resumed)
	}
	h.send(map[string]any{"v": 1, "id": "stop-wire", "op": "meeting_stop", "session_id": id})
	if got := h.receive(); !got.OK {
		t.Fatalf("stop failed: %#v", got)
	}
	h.close()
}
