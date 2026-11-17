package main

import (
    "context"
    "errors"
    "testing"

    "voicerewrite/internal/core/audio"
    meetingcore "voicerewrite/internal/core/meeting"
)

// A native callback can be queued before a pause and delivered only after
// resume. The session token alone is not sufficient; the PCM epoch must match.
func TestMeetingPacketEpochRejectsLateNativeCallbacks(t *testing.T) {
    m := &bridgeMeeting{
        runtime: meetingcore.NewSessionRuntime(),
        source: &meetingPacketSource{packets: make(chan []byte, 2)},
    }
    defer m.runtime.Stop()
    pcm := make([]byte, audio.FrameBytes)
    _, first, paused := m.runtime.Snapshot()
    if paused || first == 0 {
        t.Fatalf("initial audio epoch = %d, paused=%v", first, paused)
    }
    if err := m.feed(pcm, first); err != nil {
        t.Fatalf("first epoch rejected: %v", err)
    }
    if err := m.setPaused(true); err != nil {
        t.Fatal(err)
    }
    if got := len(m.source.packets); got != 0 {
        t.Fatalf("pause retained %d pre-pause PCM frames", got)
    }
    if err := m.feed(pcm, first); !errors.Is(err, errMeetingPaused) {
        t.Fatalf("pause allowed old native callback: %v", err)
    }
    if err := m.setPaused(false); err != nil {
        t.Fatal(err)
    }
    _, next, paused := m.runtime.Snapshot()
    if paused || next <= first {
        t.Fatalf("resume did not replace audio epoch: old=%d new=%d", first, next)
    }
    if err := m.feed(pcm, first); !errors.Is(err, errMeetingStaleEpoch) {
        t.Fatalf("old native PCM entered resumed segmentation: %v", err)
    }
    if got := len(m.source.packets); got != 0 {
        t.Fatalf("old PCM was queued after resume: %d", got)
    }
    if err := m.feed(pcm, next); err != nil {
        t.Fatalf("fresh PCM was rejected: %v", err)
    }
    if got := len(m.source.packets); got != 1 {
        t.Fatalf("fresh PCM was not queued: %d", got)
    }
    m.runtime.Stop()
    if err := m.feed(pcm, next); !errors.Is(err, context.Canceled) {
        t.Fatalf("stopped session accepted PCM: %v", err)
    }
}

func TestMeetingAudioOverloadMetricsAndLatencyReset(t *testing.T) {
    m := &bridgeMeeting{
        runtime: meetingcore.NewSessionRuntime(),
        source: &meetingPacketSource{packets: make(chan []byte, 1)},
    }
    defer m.runtime.Stop()
    _, epoch, _ := m.runtime.Snapshot()
    pcm := make([]byte, audio.FrameBytes)
    if err := m.feed(pcm, epoch); err != nil {
        t.Fatal(err)
    }
    if err := m.feed(pcm, epoch); !errors.Is(err, errMeetingBusy) {
        t.Fatalf("full native packet queue was not bounded: %v", err)
    }
    m.mu.Lock()
    if m.dropped != 1 || m.status != meetingcore.DroppedWork {
        t.Errorf("native packet drop not reflected in HUD metrics: dropped=%d status=%q", m.dropped, m.status)
    }
    m.asrFirstMS, m.asrDoneMS = 120, 240
    m.translationFirstMS, m.translationDoneMS = 300, 420
    m.mu.Unlock()
    if err := m.setPaused(true); err != nil {
        t.Fatal(err)
    }
    if err := m.setPaused(false); err != nil {
        t.Fatal(err)
    }
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.dropped != 1 {
        t.Errorf("cumulative drop count reset: %d", m.dropped)
    }
    if m.asrFirstMS != 0 || m.asrDoneMS != 0 || m.translationFirstMS != 0 || m.translationDoneMS != 0 {
        t.Errorf("old epoch latency leaked after restart: %d %d %d %d",
            m.asrFirstMS, m.asrDoneMS, m.translationFirstMS, m.translationDoneMS)
    }
}
