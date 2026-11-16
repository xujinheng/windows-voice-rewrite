package shortcut

import "testing"

func TestVoiceShortcutNormalization(t *testing.T) {
	tests := []struct {
		raw, want string
		ok        bool
	}{
		{"Mouse4", "Mouse4", true}, {"xbutton2", "Mouse5", true},
		{"f8", "F8", true}, {"F24", "F24", true},
		{"Ctrl+p", "Ctrl+P", true}, {"ctrl+alt+shift+f8", "Ctrl+Alt+Shift+F8", true},
		{"Alt+PageDown", "Alt+PageDown", true},
        {"Ctrl+Option+R", "Ctrl+Alt+R", true},
		{"f0", "", false}, {"F25", "", false}, {"A", "", false}, {"Ctrl", "", false},
		{"Shift+A", "", false}, {"Win+P", "", false}, {"Ctrl+Ctrl+P", "", false},
	}
	for _, tc := range tests {
		got, _, _, _, err := Parse(tc.raw)
		if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("input=%q got=%q err=%v want=%q", tc.raw, got, err, tc.want)
		}
	}
}

func TestDictationAndRewriteShortcutCollisions(t *testing.T) {
	if _, err := ValidateDictation("F9", "f9", "Ctrl+Alt+F10"); err == nil {
		t.Fatal("dictation/rewrite collision accepted")
	}
	if _, err := ValidateRewrite("ctrl+alt+f10", "Mouse4", "Ctrl+Alt+F10"); err == nil {
		t.Fatal("exit collision accepted")
	}
	if v, err := ValidateDictation("Ctrl+P", "Mouse5", "Ctrl+Alt+F10"); err != nil || v != "Ctrl+P" {
		t.Fatalf("combo: %q %v", v, err)
	}
	if v, err := ValidateRewrite("Mouse5", "Ctrl+P", "Ctrl+Alt+F10"); err != nil || v != "Mouse5" {
		t.Fatalf("mouse: %q %v", v, err)
	}
	if _, err := ValidateDictation("Pause", "Mouse5", "Ctrl+Alt+F10"); err == nil {
		t.Fatal("Pause must not be a hold key")
	}
}

func TestBindingReportsKeyAndModifiers(t *testing.T) {
	for _, tc := range []struct{ raw, key string }{
		{"Ctrl+Alt+R", "R"},
		{"ctrl+alt+e", "E"},
		{"Ctrl+Alt+T", "T"},
	} {
		binding, err := Bind(tc.raw)
		if err != nil || binding.Key != tc.key || !binding.Ctrl || !binding.Alt || binding.Shift || binding.Mouse {
			t.Fatalf("%s %#v %v", tc.raw, binding, err)
		}
	}
	if _, err := Bind("Shift+A"); err == nil {
		t.Fatal("shift letter accepted")
	}
	mouse, err := Bind("mouse4")
	if err != nil || !mouse.Mouse || mouse.Key != "Mouse4" {
		t.Fatalf("mouse %#v %v", mouse, err)
	}
	if id, ok := WindowsSideButton(" xbutton2 "); id != 2 || !ok {
		t.Fatalf("side button %d %v", id, ok)
	}
	if _, ok := WindowsSideButton("F9"); ok {
		t.Fatal("keyboard shortcut mapped as a mouse button")
	}
}
