package popup

import "testing"

func TestAdaptiveSizing(t *testing.T) {
	for _, tt := range []struct {
		n int
		h int32
	}{{0, 258}, {80, 258}, {200, 258}, {201, 315}, {600, 315}, {601, 430}, {1201, 500}} {
		w, h := SizeForText(tt.n)
		if w != 476 || h != tt.h {
			t.Errorf("length %d -> %dx%d, wanted 476x%d", tt.n, w, h, tt.h)
		}
	}
}

func TestClickDismissesExceptResponseAndActions(t *testing.T) {
	window := HitRect{10, 10, 486, 268}
	editor := HitRect{30, 66, 466, 202}
	buttons := []HitRect{{258, 214, 375, 252}, {383, 214, 468, 252}}
	for _, tc := range []struct {
		name    string
		x, y    int32
		dismiss bool
	}{
		{"outside left", 9, 125, true},
		{"outside right", 486, 125, true},
		{"outside top", 190, 9, true},
		{"outside bottom", 190, 268, true},
		{"header", 45, 32, true},
		{"blank margin", 18, 100, true},
		{"empty footer", 80, 230, true},
		{"body selection", 50, 92, false},
		{"body scrollbar", 465, 115, false},
		{"apply", 300, 233, false},
		{"copy", 420, 233, false},
	} {
		if got := ClickDismisses(tc.x, tc.y, window, editor, buttons); got != tc.dismiss {
			t.Errorf("%s: dismiss=%v, want %v", tc.name, got, tc.dismiss)
		}
	}
}

func TestCardControlsMatchScaledInsets(t *testing.T) {
	const scale = 1.5
	px := func(v int32) int32 { return int32(float64(v)*scale + 0.5) }
	cw, ch := int32(714), int32(400)
	editor := Editor(cw, ch, scale)
	if editor.X != px(22) || editor.Y != px(59) || editor.W != cw-px(44) || editor.H != ch-px(119) {
		t.Fatalf("editor %+v", editor)
	}
	note := Note(cw, scale)
	if note.X != px(132) || note.Y != px(16) || note.W != cw-px(190) || note.H != px(20) {
		t.Fatalf("note %+v", note)
	}
	close := CloseButton(cw, scale)
	if close.X != cw-px(51) || close.Y != px(11) || close.W != px(31) || close.H != px(29) {
		t.Fatalf("close %+v", close)
	}
	copy := CopyButton(cw, ch, scale)
	if copy.X != cw-px(119) || copy.Y != ch-px(48) || copy.W != px(98) || copy.H != px(33) {
		t.Fatalf("copy %+v", copy)
	}
	apply := ApplyButton(cw, ch, scale)
	if apply.X != cw-px(243) || apply.Y != copy.Y || apply.W != px(116) || apply.H != px(33) {
		t.Fatalf("apply %+v", apply)
	}
}
