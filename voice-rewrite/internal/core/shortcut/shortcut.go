// Package shortcut normalizes voice hotkeys stored in configuration.
// Modifier bits match the values Windows RegisterHotKey expects. Bare letters
// are rejected so a shortcut cannot capture ordinary typing.
package shortcut

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	alt      uint32 = 0x0001
	ctrl     uint32 = 0x0002
	shift    uint32 = 0x0004
	noRepeat uint32 = 0x4000

	// Alt, Ctrl and Shift are the modifier bits stored with a keyboard shortcut.
	Alt   = alt
	Ctrl  = ctrl
	Shift = shift
)

// macOS settings defaults. Windows defaults stay Mouse4, Mouse5, and Ctrl+Alt+F10.
const (
	MacDictationShortcut = "Ctrl+Alt+R"
	MacRewriteShortcut   = "Ctrl+Alt+E"
	MacTextEditShortcut  = "Ctrl+Alt+T"
)

const (
	KeyRejectedMessage      = "该按键不能用作全局录音快捷键"
	WindowsComboMessage     = "Windows 系统键组合不支持录音快捷键"
	BareKeyMessage          = "单键仅支持 F1–F24；字母、数字和方向键请搭配 Ctrl 或 Alt"
	ShiftLetterMessage      = "Shift+字母会干扰打字，请同时使用 Ctrl 或 Alt"
	EmptyShortcutMessage    = "请按下快捷键"
	CtrlRepeatMessage       = "Ctrl 重复"
	AltRepeatMessage        = "Alt 重复"
	ShiftRepeatMessage      = "Shift 重复"
	PauseReleaseMessage     = "Pause/Break 没有可靠的松开事件"
	DictationInvalidMessage = "听写快捷键无效，请重新录入（支持功能键和 Ctrl/Alt 组合键）"
	SameMouseMessage        = "听写和改写不能占用同一个鼠标侧键"
	DictationHoldMessage    = "键盘听写快捷键当前只支持按住说话模式（record_mode=hold）"
	MouseHoldMessage        = "Mouse4/Mouse5 目前仅支持 record_mode=hold"
	PauseHoldMessage        = "Pause/Break 不支持按住录音模式"
	RecordExitDifferMessage = "Record and exit hotkeys must differ"
	UnknownModifierPrefix   = "未知修饰键 "
	ShortcutCollisionSuffix = "不能与另一语音功能或退出快捷键相同"
	MouseExitPrefix         = "mouse button "
)

// FixedMessage reports a shortcut sentence a host can show unchanged.
func FixedMessage(message string) bool {
	switch message {
	case KeyRejectedMessage, WindowsComboMessage, BareKeyMessage, ShiftLetterMessage,
		EmptyShortcutMessage, CtrlRepeatMessage, AltRepeatMessage, ShiftRepeatMessage,
		PauseReleaseMessage, DictationInvalidMessage, SameMouseMessage, DictationHoldMessage,
		MouseHoldMessage, PauseHoldMessage, RecordExitDifferMessage:
		return true
	}
	return strings.HasPrefix(message, UnknownModifierPrefix) ||
		strings.HasSuffix(message, ShortcutCollisionSuffix) ||
		strings.HasPrefix(message, MouseExitPrefix)
}

var namedKeys = map[string]uint32{
	"SPACE": 0x20, "TAB": 0x09, "ENTER": 0x0D,
	"LEFT": 0x25, "UP": 0x26, "RIGHT": 0x27, "DOWN": 0x28,
	"HOME": 0x24, "END": 0x23, "PAGEUP": 0x21, "PAGEDOWN": 0x22,
	"INSERT": 0x2D, "DELETE": 0x2E,
}

func keyName(vk uint32) string {
	if vk >= 0x70 && vk <= 0x87 {
		return fmt.Sprintf("F%d", vk-0x70+1)
	}
	if vk >= 'A' && vk <= 'Z' || vk >= '0' && vk <= '9' {
		return string(rune(vk))
	}
	for name, key := range namedKeys {
		if vk == key {
			switch name {
			case "PAGEUP":
				return "PageUp"
			case "PAGEDOWN":
				return "PageDown"
			default:
				return strings.ToUpper(name[:1]) + strings.ToLower(name[1:])
			}
		}
	}
	return ""
}

// FromKey formats a physical key and Ctrl/Alt/Shift into the stored shortcut.
func FromKey(vk, modifiers uint32) (string, error) {
	name := keyName(vk)
	if name == "" {
		return "", errors.New(KeyRejectedMessage)
	}
	if modifiers & ^(alt|ctrl|shift) != 0 {
		return "", errors.New(WindowsComboMessage)
	}
	if modifiers == 0 && (vk < 0x70 || vk > 0x87) {
		return "", errors.New(BareKeyMessage)
	}
	if modifiers == shift && (vk < 0x70 || vk > 0x87) {
		return "", errors.New(ShiftLetterMessage)
	}
	parts := []string{}
	if modifiers&ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if modifiers&alt != 0 {
		parts = append(parts, "Alt")
	}
	if modifiers&shift != 0 {
		parts = append(parts, "Shift")
	}
	return strings.Join(append(parts, name), "+"), nil
}

// WindowsSideButton maps a stored shortcut to the XBUTTON id used by the mouse hook.
// Mouse4 is XBUTTON1 (1) and Mouse5 is XBUTTON2 (2).
func WindowsSideButton(s string) (uint16, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mouse4", "xbutton1":
		return 1, true
	case "mouse5", "xbutton2":
		return 2, true
	default:
		return 0, false
	}
}

// Parse returns the canonical shortcut, modifier bits, virtual key, and whether
// the value is a mouse side button. Keyboard modifiers include the no-repeat bit.
func Parse(raw string) (string, uint32, uint32, bool, error) {
	key := strings.TrimSpace(raw)
	switch strings.ToLower(key) {
	case "mouse4", "xbutton1":
		return "Mouse4", 0, 0, true, nil
	case "mouse5", "xbutton2":
		return "Mouse5", 0, 0, true, nil
	case "pause", "break":
		return "Pause", noRepeat, 0x13, false, nil
	}
	parts := strings.Split(key, "+")
	if len(parts) == 0 {
		return "", 0, 0, false, errors.New(EmptyShortcutMessage)
	}
	var modifiers uint32
	for _, token := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(token)) {
		case "ctrl", "control":
			if modifiers&ctrl != 0 {
				return "", 0, 0, false, errors.New(CtrlRepeatMessage)
			}
			modifiers |= ctrl
		case "alt", "option":
			if modifiers&alt != 0 {
				return "", 0, 0, false, errors.New(AltRepeatMessage)
			}
			modifiers |= alt
		case "shift":
			if modifiers&shift != 0 {
				return "", 0, 0, false, errors.New(ShiftRepeatMessage)
			}
			modifiers |= shift
		default:
			return "", 0, 0, false, fmt.Errorf(UnknownModifierPrefix+"%q", token)
		}
	}
	last := strings.ToUpper(strings.TrimSpace(parts[len(parts)-1]))
	var vk uint32
	if strings.HasPrefix(last, "F") {
		if n, err := strconv.Atoi(strings.TrimPrefix(last, "F")); err == nil && n >= 1 && n <= 24 {
			vk = 0x70 + uint32(n) - 1
		}
	}
	if vk == 0 && len(last) == 1 {
		if last[0] >= 'A' && last[0] <= 'Z' || last[0] >= '0' && last[0] <= '9' {
			vk = uint32(last[0])
		}
	}
	if vk == 0 {
		vk = namedKeys[last]
	}
	normalized, err := FromKey(vk, modifiers)
	if err != nil {
		return "", 0, 0, false, err
	}
	return normalized, modifiers | noRepeat, vk, false, nil
}

func validate(candidate, other, exit, label string) (string, error) {
	normalized, _, vk, mouse, err := Parse(candidate)
	if err != nil {
		return "", err
	}
	if !mouse && vk == 0x13 {
		return "", errors.New(PauseReleaseMessage)
	}
	otherValue, _, _, _, _ := Parse(other)
	exitValue, _, _, _, _ := Parse(exit)
	if normalized == otherValue || normalized == exitValue {
		return "", fmt.Errorf("%s"+ShortcutCollisionSuffix, label)
	}
	return normalized, nil
}

func ValidateDictation(candidate, rewrite, exit string) (string, error) {
	return validate(candidate, rewrite, exit, "听写快捷键")
}

func ValidateRewrite(candidate, dictation, exit string) (string, error) {
	return validate(candidate, dictation, exit, "语音指令快捷键")
}

const pauseVK uint32 = 0x13

// Startup is the keyboard and mouse registration decided before a host installs hooks.
type Startup struct {
	DictationMouse             bool
	DictationButton            uint16
	DictationMods, DictationVK uint32
	RewriteMouse               bool
	RewriteButton              uint16
	RewriteMods, RewriteVK     uint32
	ExitMods, ExitVK           uint32
}

// ValidateStartup applies the Windows launch rules in their existing order.
// Dictation parse failures keep one message. Rewrite and exit parse failures
// keep the parser's own message. An exit shortcut cannot be a mouse button.
func ValidateStartup(mode, dictation, rewrite, exit string) (Startup, error) {
	var out Startup
	dictationButton, dictationMouse := WindowsSideButton(dictation)
	rewriteButton, rewriteMouse := WindowsSideButton(rewrite)
	out.DictationMouse = dictationMouse
	out.DictationButton = dictationButton
	out.RewriteMouse = rewriteMouse
	out.RewriteButton = rewriteButton
	if !dictationMouse {
		_, mods, vk, _, err := Parse(dictation)
		if err != nil || vk == pauseVK {
			return Startup{}, errors.New(DictationInvalidMessage)
		}
		out.DictationMods, out.DictationVK = mods, vk
	}
	if dictationMouse && rewriteMouse && dictationButton == rewriteButton {
		return Startup{}, errors.New(SameMouseMessage)
	}
	if mode != "hold" && !dictationMouse {
		return Startup{}, errors.New(DictationHoldMessage)
	}
	if rewriteMouse {
		if mode != "hold" {
			return Startup{}, errors.New(MouseHoldMessage)
		}
	} else {
		_, mods, vk, _, err := Parse(rewrite)
		if err != nil {
			return Startup{}, err
		}
		out.RewriteMods, out.RewriteVK = mods, vk
	}
	_, exitMods, exitVK, exitMouse, err := Parse(exit)
	if err != nil {
		return Startup{}, err
	}
	if exitMouse {
		return Startup{}, fmt.Errorf(MouseExitPrefix+"%q must use the mouse hook", exit)
	}
	out.ExitMods, out.ExitVK = exitMods, exitVK
	sameRewriteExit := !rewriteMouse && out.RewriteMods == exitMods && out.RewriteVK == exitVK
	sameRewriteDictation := !rewriteMouse && !dictationMouse && out.RewriteMods == out.DictationMods && out.RewriteVK == out.DictationVK
	sameDictationExit := !dictationMouse && exitMods == out.DictationMods && exitVK == out.DictationVK
	if sameRewriteExit || sameRewriteDictation || sameDictationExit {
		return Startup{}, errors.New(RecordExitDifferMessage)
	}
	if mode == "hold" && !rewriteMouse && out.RewriteVK == pauseVK {
		return Startup{}, errors.New(PauseHoldMessage)
	}
	return out, nil
}

// Binding is the keyboard or mouse action described by a stored shortcut.
// Key is the canonical name, such as R, F8, or PageDown. Virtual-key numbers
// stay inside Parse for the Windows host.
type Binding struct {
	Normalized string
	Key        string
	Mouse      bool
	Ctrl       bool
	Alt        bool
	Shift      bool
}

func Bind(raw string) (Binding, error) {
	normalized, modifiers, _, mouse, err := Parse(raw)
	if err != nil {
		return Binding{}, err
	}
	key := normalized
	if i := strings.LastIndex(normalized, "+"); i >= 0 {
		key = normalized[i+1:]
	}
	return Binding{
		Normalized: normalized,
		Key:        key,
		Mouse:      mouse,
		Ctrl:       modifiers&Ctrl != 0,
		Alt:        modifiers&Alt != 0,
		Shift:      modifiers&Shift != 0,
	}, nil
}
