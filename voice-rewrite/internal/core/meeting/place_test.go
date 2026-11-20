package meeting

import (
	"testing"

	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/desktop"
)

func TestPlaceClampsRemovedMonitor(t *testing.T) {
	p := Preferences{X: -2000, Y: 1800, Width: 1800, Height: 1400}
	x, y, w, h := Place(p, desktop.WorkArea{Left: 0, Top: 30, Right: 1024, Bottom: 768}, 1)
	if x != 0 || y != 30 || w != 1024 || h != 738 {
		t.Fatalf("offscreen geometry = %d,%d %dx%d", x, y, w, h)
	}
	p = Preferences{X: -1200, Y: 50, Width: 700, Height: 750}
	x, y, w, h = Place(p, desktop.WorkArea{Left: -1280, Top: 0, Right: 0, Bottom: 1000}, 1)
	if x != -1200 || y != 50 || w != 700 || h != 750 {
		t.Fatalf("valid negative geometry = %d,%d %dx%d", x, y, w, h)
	}
	p = Preferences{Width: -4, Height: 2}
	_, _, w, h = Place(p, desktop.WorkArea{Right: 1920, Bottom: 1080}, 1)
	if w != 640 || h != 720 {
		t.Fatalf("invalid size = %dx%d", w, h)
	}
}
