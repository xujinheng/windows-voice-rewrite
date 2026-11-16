package settingsform

import "testing"

func TestSettingsRailAndTitlesStayDistinct(t *testing.T) {
	if RailFill(500) != (Box{0, 0, 194, 500}) || RailDivider(500) != (Box{193, 0, 1, 500}) {
		t.Fatal("rail")
	}
	if DirectFieldRadius == FallbackFieldRadius || NavRadius != FallbackFieldRadius {
		t.Fatal("radius")
	}
	if RailColor(true) == 0x17191E || RailColor(false) == 0xF6F7F9 || RailColor(true) == RailColor(false) {
		t.Fatal("rail color")
	}
	if MutedControls(2, 3, 4) != [3]uintptr{2, 3, 4} || !MutedControl(2, 2, 3, 4) || !MutedControl(3, 2, 3, 4) || !MutedControl(4, 2, 3, 4) || MutedControl(9, 2, 3, 4) {
		t.Fatal("muted")
	}
	if !MutedControl(0, 0, 3, 4) || MutedControl(0, 2, 3, 4) {
		t.Fatal("zero hint")
	}
	x, y, cut, h := DirectTitle()
	fallback := FallbackTitle()
	if x != 220 || y != 35 || cut != 245 || h != 39 || fallback != (Edges{220, 34, 15, 80}) || y == fallback.Top {
		t.Fatalf("title %d %d %d %d %#v", x, y, cut, h, fallback)
	}
	sx, sy, scut, sh := DirectSubtitle()
	gdi := FallbackSubtitle()
	if sx != 222 || sy != 79 || scut != 244 || sh != 23 || gdi != (Edges{222, 80, 15, 110}) {
		t.Fatalf("subtitle %d %d %#v", sy, scut, gdi)
	}
	fx, fy, fw, fh := DirectFooterRule()
	if fx != 194 || fy != 77 || fw != 194 || fh != 1 || DirectSection() != (Box{224, 132, 235, 26}) {
		t.Fatal("section")
	}
	item := Edges{Left: 10, Top: 20, Right: 180, Bottom: 55}
	if NavAccent(item) != (Edges{12, 29, 15, 46}) || NavLabelLeft(item.Left) != 28 || NavFocus(item) != (Edges{18, 24, 174, 51}) {
		t.Fatal("nav item")
	}
}
