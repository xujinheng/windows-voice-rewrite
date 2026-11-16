package desktop

import "testing"

func TestPopupPlacementNegativeMonitor(t *testing.T) {
	secondary := WorkArea{-1920, -120, 0, 960}
	x, y, w, h := PlacePopup(-15, 910, 576, 620, secondary)
	if x != -576 || y != 340 || w != 576 || h != 620 {
		t.Fatalf("secondary %d,%d %dx%d", x, y, w, h)
	}
	x, y, w, h = PlacePopup(-1910, -100, 2200, 1500, secondary)
	if x != -1896 || y != -96 || w != 1896 || h != 1056 {
		t.Fatalf("oversize %d,%d %dx%d", x, y, w, h)
	}
}

func TestCenterStaysOnNegativeMonitor(t *testing.T) {
	x, y := Center(800, 600, WorkArea{-1920, -120, 0, 960})
	if x != -1360 || y != 120 {
		t.Fatalf("center %d,%d", x, y)
	}
	x, y = Center(3000, 2000, WorkArea{-1920, -120, 0, 960})
	if x != -1920 || y != -120 {
		t.Fatalf("oversize %d,%d", x, y)
	}
}

func TestFitSizeSmallWorkArea(t *testing.T) {
	work := WorkArea{0, 0, 1024, 728}
	for _, s := range []struct{ w, h, ew, eh int32 }{{714, 720, 714, 704}, {1428, 1440, 1000, 704}, {1071, 1080, 1000, 704}} {
		w, h := Fit(s.w, s.h, work, 12)
		if w != s.ew || h != s.eh {
			t.Errorf("%v got %dx%d", s, w, h)
		}
	}
}
