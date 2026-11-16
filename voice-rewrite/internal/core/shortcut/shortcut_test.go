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

func TestStartupHotkeyRulesKeepWindowsOrder(t *testing.T) {
	ready, err := ValidateStartup("hold", "Mouse4", "Mouse5", "Ctrl+Alt+F10")
	if err != nil || !ready.DictationMouse || !ready.RewriteMouse || ready.DictationButton != 1 || ready.RewriteButton != 2 {
		t.Fatalf("defaults %#v %v", ready, err)
	}
	if ready.ExitVK != 0x79 || ready.ExitMods != Ctrl|Alt|0x4000 {
		t.Fatalf("exit registration vk=%#x mods=%#x", ready.ExitVK, ready.ExitMods)
	}
	keyboard, err := ValidateStartup("hold", "Ctrl+Alt+R", "F8", "Ctrl+Alt+F10")
	if err != nil || keyboard.DictationMouse || keyboard.DictationVK != 'R' || keyboard.RewriteVK != 0x77 {
		t.Fatalf("keyboard %#v %v", keyboard, err)
	}
	cases := []struct{ mode, dictation, rewrite, exit, want string }{
		{"hold", "Pause", "Mouse5", "Ctrl+Alt+F10", "听写快捷键无效，请重新录入（支持功能键和 Ctrl/Alt 组合键）"},
		{"hold", "Shift+A", "Mouse5", "Ctrl+Alt+F10", "听写快捷键无效，请重新录入（支持功能键和 Ctrl/Alt 组合键）"},
		{"hold", "Mouse4", "xbutton1", "Ctrl+Alt+F10", "听写和改写不能占用同一个鼠标侧键"},
		{"toggle", "Ctrl+P", "Mouse5", "Ctrl+Alt+F10", "键盘听写快捷键当前只支持按住说话模式（record_mode=hold）"},
		{"toggle", "Mouse4", "Mouse5", "Ctrl+Alt+F10", "Mouse4/Mouse5 目前仅支持 record_mode=hold"},
		{"hold", "Mouse4", "Shift+A", "Ctrl+Alt+F10", "Shift+字母会干扰打字，请同时使用 Ctrl 或 Alt"},
		{"hold", "Mouse4", "Ctrl+Alt+F10", "Ctrl+Alt+F10", "Record and exit hotkeys must differ"},
		{"hold", "Ctrl+Alt+F10", "Mouse5", "Ctrl+Alt+F10", "Record and exit hotkeys must differ"},
		{"hold", "F9", "F9", "Ctrl+Alt+F10", "Record and exit hotkeys must differ"},
		{"hold", "Mouse4", "Pause", "Ctrl+Alt+F10", "Pause/Break 不支持按住录音模式"},
		{"hold", "Mouse4", "Mouse5", "Mouse4", `mouse button "Mouse4" must use the mouse hook`},
	}
	for _, tc := range cases {
		_, err := ValidateStartup(tc.mode, tc.dictation, tc.rewrite, tc.exit)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s/%s/%s/%s: got %v want %s", tc.mode, tc.dictation, tc.rewrite, tc.exit, err, tc.want)
		}
	}
	if _, err := ValidateStartup("toggle", "Mouse4", "Pause", "Ctrl+Alt+F10"); err != nil {
		t.Fatalf("toggle pause rewrite is registered by Windows: %v", err)
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
