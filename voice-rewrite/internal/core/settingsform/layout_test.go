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

func TestClampScroll(t *testing.T) {
	if ClampScroll(-4, 10) != 0 || ClampScroll(3, 10) != 3 || ClampScroll(40, 10) != 10 || ClampScroll(2, -1) != 0 {
		t.Fatal("scroll clamp")
	}
}
