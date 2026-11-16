package settingsform

import "testing"

func TestResponsiveSettingsRect(t *testing.T) {
	input := Spec{X: 350, Y: 173, W: 325, H: 34, Stretch: true}
	right := Spec{X: 687, Y: 173, W: 92, H: 34, Right: true}
	for _, width := range []int32{816, 896, 1056} {
		x, y, w, h := Layout(input, width)
		if x != 350 || y != 173 || h != 34 || w != 325+(width-816) {
			t.Fatalf("edit at %d: %d %d %d %d", width, x, y, w, h)
		}
		bx, _, bw, _ := Layout(right, width)
		if bx != 687+(width-DesignWidth) || bw != 92 || x+w+12 != bx {
			t.Fatalf("misaligned at %d", width)
		}
	}
}

func TestMinimumSettingsWindowFitsWorkArea(t *testing.T) {
	w, h := MinimumSize(1, 2000, 2000)
	if w != MinWidth || h != MinHeight {
		t.Fatalf("design minimum %d %d", w, h)
	}
	w, h = MinimumSize(2, 2000, 2000)
	if w != 960 || h != 840 {
		t.Fatalf("scaled minimum %d %d", w, h)
	}
	w, h = MinimumSize(2, 100, 80)
	if w != 88 || h != 68 {
		t.Fatalf("work area minimum %d %d", w, h)
	}
}

func TestClampScroll(t *testing.T) {
	if ClampScroll(-4, 10) != 0 || ClampScroll(3, 10) != 3 || ClampScroll(40, 10) != 10 || ClampScroll(2, -1) != 0 {
		t.Fatal("scroll clamp")
	}
}
