package main

import (
	"testing"

	meetingcore "voicerewrite/internal/core/meeting"
)

func TestMeetingPreferencesClampRemovedMonitor(t *testing.T) {
	p := meetingcore.Preferences{X: -2000, Y: 1800, Width: 1800, Height: 1400}
	x, y, w, h := meetingGeometry(p, desktopWorkArea{left: 0, top: 30, right: 1024, bottom: 768}, 1)
	if x != 0 || y != 30 || w != 1024 || h != 738 {
		t.Fatalf("offscreen geometry = %d,%d %dx%d", x, y, w, h)
	}
	p = meetingcore.Preferences{X: -1200, Y: 50, Width: 700, Height: 750}
	x, y, w, h = meetingGeometry(p, desktopWorkArea{left: -1280, top: 0, right: 0, bottom: 1000}, 1)
	if x != -1200 || y != 50 || w != 700 || h != 750 {
		t.Fatalf("valid negative geometry = %d,%d %dx%d", x, y, w, h)
	}
	p = meetingcore.Preferences{Width: -4, Height: 2}
	_, _, w, h = meetingGeometry(p, desktopWorkArea{right: 1920, bottom: 1080}, 1)
	if w != 640 || h != 720 {
		t.Fatalf("invalid size = %dx%d", w, h)
	}
}
