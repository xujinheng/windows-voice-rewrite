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

func TestSettingsFooterAndScrollStayPut(t *testing.T) {
	if x, w := AnchoredX(AnchorSave, 800, 124, 0); x != 220 || w != 124 {
		t.Fatalf("save %d %d", x, w)
	}
	if x, _ := AnchoredX(AnchorSave, 600, 124, 0); x != 12 {
		t.Fatalf("narrow save %d", x)
	}
	if x, _ := AnchoredX(AnchorAdvanced, 800, 112, 0); x != 358 {
		t.Fatalf("advanced %d", x)
	}
	if x, _ := AnchoredX(AnchorAdvanced, 600, 112, 0); x != 149 {
		t.Fatalf("narrow advanced %d", x)
	}
	if x, _ := AnchoredX(AnchorClose, 800, 94, 0); x != 688 {
		t.Fatalf("close %d", x)
	}
	if x, _ := AnchoredX(AnchorClose, 600, 94, 0); x != 276 {
		t.Fatalf("narrow close %d", x)
	}
	if x, w := AnchoredX(AnchorHint, 400, 400, 9); x != 14 || w != 374 {
		t.Fatalf("hint %d %d", x, w)
	}
	if delta, edge := ScrollStep(0, 100); delta != -24 || edge != 0 {
		t.Fatal("line up")
	}
	if delta, edge := ScrollStep(3, 100); delta != 52 || edge != 0 {
		t.Fatal("page down")
	}
	if _, edge := ScrollStep(6, 100); edge != -1 {
		t.Fatal("top")
	}
	if _, edge := ScrollStep(7, 100); edge != 1 {
		t.Fatal("bottom")
	}
	if WheelScroll(100, 240) != -44 || WheelScroll(100, -120) != 172 {
		t.Fatal("wheel")
	}
	if FocusRow(4, false, false) != 1 || FocusRow(0, true, false) != 0 || FocusRow(0, false, true) != 1 || FocusRow(-1, false, false) != -1 {
		t.Fatal("focus row")
	}
}

func TestClampScroll(t *testing.T) {
	if ClampScroll(-4, 10) != 0 || ClampScroll(3, 10) != 3 || ClampScroll(40, 10) != 10 || ClampScroll(2, -1) != 0 {
		t.Fatal("scroll clamp")
	}
	if ScrollLimit(900, 1000) != 0 || ScrollLimit(900, 400) != 500 || ScrollLimit(900, 0) != 900 {
		t.Fatal("scroll limit")
	}
}
