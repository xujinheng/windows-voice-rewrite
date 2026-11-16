//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"voicerewrite/internal/contract"
	"voicerewrite/internal/core/desktop"
	"voicerewrite/internal/core/serviceprobe"
	"voicerewrite/internal/core/settingsform"
	"voicerewrite/internal/core/shortcut"
	"voicerewrite/internal/core/ux"
)

// A small, native settings surface; it does not host a browser or intercept
// the editor focus until deliberately opened from the tray menu.
const (
	uxWMCommand               = 0x0111
	uxWMSize                  = 0x0005
	uxWMVScroll               = 0x0115
	uxWMHScroll               = 0x0114
	uxWMMouseWheel            = 0x020A
	uxSIFRange                = 1
	uxSIFPage                 = 2
	uxSIFPos                  = 4
	uxSIFTrackPos             = 16
	uxSBHorz                  = 0
	uxSBVert                  = 1
	uxWMGetMinMaxInfo         = 0x0024
	uxWSThickFrame            = 0x00040000
	uxWMSettingChange         = 0x001A
	uxWMThemeChanged          = 0x031A
	uxWMClose                 = 0x0010
	uxWMEraseBkgnd            = 0x0014
	uxWMPaint                 = 0x000F
	uxWMNCDestroy             = 0x0082
	uxWMColorStatic           = 0x0138
	uxWMColorEdit             = 0x0133
	uxWMDrawItem              = 0x002B
	uxBSOwnerDraw             = 0x000B
	uxEMSetMargins            = 0x00D3
	uxWMProbeDone             = 0x8000 + 104
	uxShortcutInput           = 9106
	uxRewriteInput            = 9108
	WM_SETTINGS_CAPTURE_MOUSE = 0x8000 + 105
	uxMicrophoneInput         = 9107
	uxASRURLInput             = 9130
	uxASRModelInput           = 9131
	uxASRKeyInput             = 9132
	uxLLMURLInput             = 9133
	uxLLMModelInput           = 9134
	uxLLMKeyInput             = 9135
	uxCBAddString             = 0x0143
	uxCBSetCurSel             = 0x014E
	uxCBGetCurSel             = 0x0147
	uxCBSDropDownList         = 0x0003
	uxWSChild                 = 0x40000000
	uxWSVisible               = 0x10000000
	uxWSTabStop               = 0x00010000
	uxESAutoScroll            = 0x0080
	uxESPassword              = 0x0020
	uxWSBorder                = 0x00800000
	uxWSCaption               = 0x00C00000
	uxWSSysMenu               = 0x00080000
	uxWSMinimizeBox           = 0x00020000
)

var (
	uxGetWindowText  = user32.NewProc("GetWindowTextW")
	uxSetTextColor   = gdi32.NewProc("SetTextColor")
	uxSelectObject   = gdi32.NewProc("SelectObject")
	uxRoundRect      = gdi32.NewProc("RoundRect")
	uxGetStockObject = gdi32.NewProc("GetStockObject")
	uxSetBkMode      = gdi32.NewProc("SetBkMode")
	uxWindowCallback = syscall.NewCallback(uxSettingsWndProc)
	uxActiveSettings *uxSettings
)

type uxSettings struct {
	window                                  uintptr
	app                                     *appState
	inputs                                  [6]uintptr
	shortcut                                uintptr
	rewriteShortcut                         uintptr
	capturingShortcut                       uintptr
	captureButtons                          [2]uintptr
	microphone                              uintptr
	microphoneIDs                           []int
	hint                                    uintptr
	font, titleFont, sectionFont, mutedFont uintptr
	background                              uintptr
	cardBrush                               uintptr
	editBrush                               uintptr
	palette                                 uiPalette
	cardStatics                             map[uintptr]bool
	probeEdits                              map[uintptr]int
	probeMu                                 sync.Mutex
	probeResult                             [2]string
	probeStatus                             [2]uintptr
	probeSerial                             [2]uint64
	controls                                []uxControl
	scale                                   float64
	scrollX, scrollY                        int32
	renderer                                *resultRenderer
	page                                    int
	focusRow                                int
	pages                                   [3][]uintptr
	navButtons                              [3]uintptr
}

// Every child control is reflowed after a resize. No hard-coded physical
// coordinates may escape uxLayoutRect; the data is in 96-DPI logical units.
type uxControl struct {
	hwnd       uintptr
	x, y, w, h int32
	stretch    bool
	right      bool
	id         uintptr
}

// 96-DPI logical coordinate scrolling avoids inaccessible buttons on small
// work areas and 125%–200% display scaling.
const uxContentWidth, uxContentHeight int32 = settingsform.DesignWidth, settingsform.DesignHeight

type uxScrollInfo struct {
	Size, Mask    uint32
	Min, Max      int32
	Page          uint32
	Pos, TrackPos int32
}

func (p *uxSettings) layout() {
	if p == nil || p.window == 0 {
		return
	}
	var rect rect32
	procGetClientRect.Call(p.window, uintptr(unsafe.Pointer(&rect)))
	viewW, viewH := int32(float64(rect.Right-rect.Left)/p.scale), int32(float64(rect.Bottom-rect.Top)/p.scale)
	if viewW < 1 {
		viewW = 1
	}
	if viewH < 1 {
		viewH = 1
	}
	maxX, maxY := uxContentWidth-viewW, uxContentHeight-viewH
	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}
	p.scrollX = settingsform.ClampScroll(p.scrollX, maxX)
	p.scrollY = settingsform.ClampScroll(p.scrollY, maxY)
	for _, spec := range []struct {
		axis               uintptr
		content, page, pos int32
	}{
		{uxSBHorz, uxContentWidth, viewW, p.scrollX},
		{uxSBVert, uxContentHeight, viewH, p.scrollY},
	} {
		info := uxScrollInfo{Size: uint32(unsafe.Sizeof(uxScrollInfo{})), Mask: uxSIFRange | uxSIFPage | uxSIFPos,
			Min: 0, Max: spec.content - 1, Page: uint32(spec.page), Pos: spec.pos}
		procSetScrollInfoUX.Call(p.window, spec.axis, uintptr(unsafe.Pointer(&info)), 1)
	}
	contentW := viewW
	if contentW < uxContentWidth {
		contentW = uxContentWidth
	}
	sc := func(v int32) uintptr { return uintptr(int32(float64(v)*p.scale + 0.5)) }
	for _, ctrl := range p.controls {
		x, y, w, h := settingsform.Layout(settingsform.Spec{X: ctrl.x, Y: ctrl.y, W: ctrl.w, H: ctrl.h, Stretch: ctrl.stretch, Right: ctrl.right}, contentW)
		x -= p.scrollX
		// Keep the primary footer actions horizontally accessible even when
		// the form itself needs horizontal scrolling at high DPI.
		switch ctrl.id {
		case contract.SaveButton:
			x = 220
			if viewW < 650 {
				x = 12
			}
		case contract.AdvancedButton:
			x = 358
			if viewW < 650 {
				x = 149
			}
		case contract.CloseButton:
			x = viewW - w - 18
			if viewW < 650 {
				x = 276
			}
		case contract.NavSpeech, contract.NavAI, contract.NavInput:
			x = ctrl.x
		}
		if ctrl.hwnd == p.hint {
			x = 223
			if viewW < 650 {
				x = 14
			}
			if w > viewW-x-12 {
				w = viewW - x - 12
			}
		}
		if ctrl.id == contract.NavSpeech || ctrl.id == contract.NavAI || ctrl.id == contract.NavInput {
			procMoveWindow.Call(ctrl.hwnd, sc(x), sc(y), sc(w), sc(h), 1)
		} else {
			procMoveWindow.Call(ctrl.hwnd, sc(x), sc(y-p.scrollY), sc(w), sc(h), 1)
		}
	}
	procInvalidateRectSettings.Call(p.window, 0, 1)
}

// switchPage only hides controls; input values and in-flight probe results
// remain intact so navigation cannot discard unsaved changes.
func (p *uxSettings) switchPage(next int) {
	if p == nil || next < 0 || next >= len(p.pages) {
		return
	}
	if next != p.page {
		p.stopShortcutCapture()
	}
	p.page = next
	p.focusRow = -1
	for page, windows := range p.pages {
		for _, handle := range windows {
			how := uintptr(0) // SW_HIDE
			if page == next {
				how = SW_SHOW
			}
			if handle != 0 {
				procShowWindow.Call(handle, how)
			}
		}
	}
	for _, hwnd := range p.navButtons {
		if hwnd != 0 {
			refreshNativeWindow(hwnd)
		}
	}
	if p.window != 0 {
		refreshNativeWindow(p.window)
	}
}

func (p *uxSettings) scroll(axis, request uintptr) {
	var rect rect32
	procGetClientRect.Call(p.window, uintptr(unsafe.Pointer(&rect)))
	view := int32(float64(rect.Bottom-rect.Top) / p.scale)
	content := uxContentHeight
	pos := &p.scrollY
	if axis == uxSBHorz {
		view = int32(float64(rect.Right-rect.Left) / p.scale)
		content = uxContentWidth
		pos = &p.scrollX
	}
	max := content - view
	if max < 0 {
		max = 0
	}
	var delta int32
	switch request & 0xffff {
	case 0:
		delta = -24
	case 1:
		delta = 24
	case 2:
		delta = -view + 48
	case 3:
		delta = view - 48
	case 6:
		*pos = 0
	case 7:
		*pos = max
	case 4, 5:
		info := uxScrollInfo{Size: uint32(unsafe.Sizeof(uxScrollInfo{})), Mask: uxSIFTrackPos}
		procGetScrollInfoUX.Call(p.window, axis, uintptr(unsafe.Pointer(&info)))
		*pos = info.TrackPos
	}
	*pos = settingsform.ClampScroll(*pos+delta, max)
	p.layout()
}

var (
	procMoveWindow             = user32.NewProc("MoveWindow")
	procSetScrollInfoUX        = user32.NewProc("SetScrollInfo")
	procGetScrollInfoUX        = user32.NewProc("GetScrollInfo")
	procInvalidateRectSettings = user32.NewProc("InvalidateRect")
)

type uxMinMaxInfo struct{ Reserved, MaxSize, MaxPos, MinSize, MaxTrack trayPoint }

func uxPtr(s string) *uint16 { ptr, _ := syscall.UTF16PtrFromString(s); return ptr }

func uxWinText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLength.Call(hwnd)
	if n > 4096 {
		n = 4096
	}
	buf := make([]uint16, n+1)
	uxGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}
func uxSetText(hwnd uintptr, text string) {
	if hwnd == 0 {
		return
	}
	ptr := uxPtr(text)
	procSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(ptr)))
	runtime.KeepAlive(ptr)
}

// The settings capture buttons take one physical key/chord or side mouse
// button. Key events are intercepted BEFORE IsDialogMessage/TranslateMessage
// so they never enter an EDIT box as text.
func (p *uxSettings) stopShortcutCapture() {
	if p == nil {
		return
	}
	p.capturingShortcut = 0
	sideShortcutCapture.Store(0)
	for _, button := range p.captureButtons {
		if button != 0 {
			refreshNativeWindow(button)
		}
	}
}
func (p *uxSettings) beginShortcutCapture(id uintptr) {
	p.stopShortcutCapture()
	p.capturingShortcut = id
	sideShortcutCapture.Store(uint32(id))
	for _, button := range p.captureButtons {
		if button != 0 {
			refreshNativeWindow(button)
		}
	}
	uxSetText(p.hint, "正在监听按键… 按下键盘组合键、F1–F24 或 Mouse4/Mouse5；Esc 取消")
}
func uxCurrentShortcutModifiers() uint32 {
	var mods uint32
	for _, m := range []struct {
		vk   uintptr
		flag uint32
	}{
		{VK_CONTROL, shortcut.Ctrl}, {VK_MENU, shortcut.Alt}, {VK_SHIFT, shortcut.Shift},
	} {
		state, _, _ := procGetAsyncKeyState.Call(m.vk)
		if state&0x8000 != 0 {
			mods |= m.flag
		}
	}
	return mods
}
func (p *uxSettings) acceptShortcutKey(vk, mods uint32) {
	if p == nil || p.capturingShortcut == 0 {
		return
	}
	if vk == 0x10 || vk == 0x11 || vk == 0x12 || vk == 0xA0 || vk == 0xA1 ||
		vk == 0xA2 || vk == 0xA3 || vk == 0xA4 || vk == 0xA5 {
		return
	}
	winL, _, _ := procGetAsyncKeyState.Call(0x5B)
	winR, _, _ := procGetAsyncKeyState.Call(0x5C)
	if winL&0x8000 != 0 || winR&0x8000 != 0 {
		uxSetText(p.hint, "Windows 系统键组合不可录入，请使用 Ctrl/Alt/Shift")
		return
	}
	candidate, err := shortcut.FromKey(vk, mods)
	if err != nil {
		uxSetText(p.hint, "无法录入："+err.Error())
		return
	}
	p.acceptShortcut(candidate)
}
func (p *uxSettings) acceptShortcut(candidate string) {
	if p == nil || p.capturingShortcut == 0 {
		return
	}
	var normalized string
	var err error
	var target uintptr
	if p.capturingShortcut == contract.CaptureRewrite {
		target = p.rewriteShortcut
		normalized, err = shortcut.ValidateRewrite(candidate, uxWinText(p.shortcut), p.app.cfg.HotkeyExit)
	} else {
		target = p.shortcut
		normalized, err = shortcut.ValidateDictation(candidate, uxWinText(p.rewriteShortcut), p.app.cfg.HotkeyExit)
	}
	if err != nil {
		uxSetText(p.hint, "无法录入："+err.Error())
		return
	}
	uxSetText(target, normalized)
	p.stopShortcutCapture()
	uxSetText(p.hint, "已录入 "+normalized+"。点击「保存设置」并重启软件后生效。")
}
func uxSettingsKeyMessage(msg winMSG) bool {
	p := uxActiveSettings
	if p == nil || p.window == 0 {
		return false
	}
	if msg.Hwnd != p.window {
		child, _, _ := procIsChild.Call(p.window, msg.Hwnd)
		if child == 0 {
			return false
		}
	}
	if p.capturingShortcut != 0 && (msg.Message == WM_KEYDOWN || msg.Message == 0x0104) {
		if msg.WParam == VK_ESCAPE {
			p.stopShortcutCapture()
			uxSetText(p.hint, "已取消快捷键录入")
		} else {
			p.acceptShortcutKey(uint32(msg.WParam), uxCurrentShortcutModifiers())
		}
		return true
	}
	if msg.Message != WM_KEYDOWN || msg.WParam != VK_ESCAPE {
		return false
	}
	procDestroyWindow.Call(p.window)
	return true
}

func (p *uxSettings) refreshTheme() {
	if p == nil || p.window == 0 {
		return
	}
	palette := resultCardPalette()
	if palette == p.palette {
		return
	}
	p.background = refreshPaletteBrush(p.background, palette, palette.background)
	p.cardBrush = refreshPaletteBrush(p.cardBrush, palette, palette.surface)
	p.editBrush = refreshPaletteBrush(p.editBrush, palette, palette.field)
	p.palette = palette
	if p.renderer != nil {
		p.renderer.updateColors(palette.dark)
	}
	for _, edit := range p.inputs {
		resultStyleNativeEdit(edit, palette.dark)
	}
	resultStyleNativeEdit(p.microphone, palette.dark)
	applyWindowPalette(p.window, palette)
	refreshNativeWindow(p.window)
	for _, c := range p.controls {
		refreshNativeWindow(c.hwnd)
	}
}

func uxSettingsWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	p := uxActiveSettings
	switch uint32(msg) {
	case uxWMSettingChange, uxWMThemeChanged:
		if p != nil && p.window == hwnd {
			p.refreshTheme()
		}
		return 0
	case uxWMVScroll, uxWMHScroll:
		if p != nil && p.window == hwnd {
			axis := uintptr(uxSBVert)
			if uint32(msg) == uxWMHScroll {
				axis = uxSBHorz
			}
			p.scroll(axis, wParam)
		}
		return 0
	case uxWMMouseWheel:
		if p != nil && p.window == hwnd {
			delta := int32(int16((wParam >> 16) & 0xffff))
			p.scrollY -= delta / 120 * 72
			p.layout()
		}
		return 0
	case uxWMSize:
		if p != nil && p.window == hwnd {
			if p.renderer != nil {
				p.renderer.resized()
			}
			p.layout()
		}
		return 0
	case uxWMGetMinMaxInfo:
		if p != nil && lParam != 0 {
			bounds := (*uxMinMaxInfo)(unsafe.Pointer(lParam))
			work := uxWorkAreaAtCursor()
			minW, minH := settingsform.MinimumSize(p.scale, work.Right-work.Left, work.Bottom-work.Top)
			bounds.MinSize = trayPoint{X: minW, Y: minH}
		}
		return 0
	case uxWMClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case uxWMNCDestroy:
		if p != nil && p.window == hwnd {
			p.stopShortcutCapture()
			uxActiveSettings = nil
			resultRemoveReadyProperty.Call(hwnd, uintptr(unsafe.Pointer(resultReadyProperty)))
			if p.renderer != nil {
				p.renderer.close()
				p.renderer = nil
			}
			if p.font != 0 {
				procDeleteObject.Call(p.font)
			}
			if p.titleFont != 0 {
				procDeleteObject.Call(p.titleFont)
			}
			if p.sectionFont != 0 {
				procDeleteObject.Call(p.sectionFont)
			}
			if p.mutedFont != 0 {
				procDeleteObject.Call(p.mutedFont)
			}
			if p.background != 0 {
				procDeleteObject.Call(p.background)
			}
			if p.cardBrush != 0 {
				procDeleteObject.Call(p.cardBrush)
			}
			if p.editBrush != 0 {
				procDeleteObject.Call(p.editBrush)
			}
		}
		return 0
	case uxWMEraseBkgnd:
		if p != nil && p.window == hwnd && p.background != 0 {
			var r rect32
			procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
			procFillRect.Call(wParam, uintptr(unsafe.Pointer(&r)), p.background)
			return 1
		}
	case uxWMPaint:
		if p != nil && p.window == hwnd {
			var ps handyPaintStruct
			hdc, _, _ := procBeginPaintHandy.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			if hdc != 0 {
				if p.renderer != nil && p.renderer.drawSettings(p.page, p.scrollX, p.scrollY, p.focusRow) {
					resultSetReadyProperty.Call(hwnd, uintptr(unsafe.Pointer(resultReadyProperty)), 1)
				} else {
					p.drawSettingsFallback(hdc)
				}
				procEndPaintHandy.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			}
			return 0
		}
	case uxWMDrawItem:
		if p != nil && p.window == hwnd && lParam != 0 {
			item := (*drawItemStruct)(unsafe.Pointer(lParam))
			if item.CtlID >= contract.NavSpeech && item.CtlID <= contract.NavInput {
				if p.drawSidebarItem(item, int(item.CtlID-contract.NavSpeech)) {
					return 1
				}
			}
			label, primary := contract.ButtonLabel(int(item.CtlID), p.capturingShortcut == uintptr(item.CtlID))
			if label != "" && uiDrawActionButton(item, p.palette, p.font, label, primary) {
				return 1
			}
		}
	case uxWMColorStatic:
		if p != nil && p.window == hwnd && p.background != 0 {
			color := p.palette.text
			if lParam == p.hint || lParam == p.probeStatus[0] || lParam == p.probeStatus[1] {
				color = p.palette.muted
			}
			uxSetTextColor.Call(wParam, p.palette.color(color))
			uxSetBkMode.Call(wParam, TRANSPARENT)
			if p.cardStatics[lParam] {
				return p.cardBrush
			}
			return p.background
		}
	case uxWMColorEdit:
		if p != nil && p.window == hwnd && p.editBrush != 0 {
			uxSetTextColor.Call(wParam, p.palette.color(p.palette.text))
			procSetBkColor.Call(wParam, p.palette.color(p.palette.field))
			return p.editBrush
		}
	case uxWMProbeDone:
		if p != nil && p.window == hwnd && wParam < 2 {
			idx := int(wParam)
			p.probeMu.Lock()
			serial := p.probeSerial[idx]
			result := p.probeResult[idx]
			p.probeMu.Unlock()
			if serial == uint64(lParam) {
				uxSetText(p.probeStatus[idx], result)
			}
		}
		return 0
	case uxWMCommand:
		if p != nil && p.window == hwnd {
			// Native focus events keep the D2D field outline keyboard-visible.
			notification := (wParam >> 16) & 0xffff
			if notification == 0x0100 || notification == 0x0200 { // EN_SETFOCUS/EN_KILLFOCUS
				if notification == 0x0200 {
					p.focusRow = -1
				} else {
					p.focusRow = -1
					for idx, edit := range p.inputs {
						if edit == lParam {
							p.focusRow = idx % 3
						}
					}
					if p.rewriteShortcut == lParam {
						p.focusRow = 0
					}
					if p.shortcut == lParam {
						p.focusRow = 1
					}
				}
				refreshNativeWindow(hwnd)
			}
			if (wParam>>16)&0xffff == 0x0300 { // EN_CHANGE
				if idx, ok := p.probeEdits[lParam]; ok {
					p.probeMu.Lock()
					p.probeSerial[idx]++ // reject in-flight stale response
					p.probeMu.Unlock()
					uxSetText(p.probeStatus[idx], serviceprobe.StatusStale)
				}
			}
			switch action, page := contract.Command(int(wParam & 0xffff)); action {
			case contract.ActionPage:
				p.switchPage(page)
				return 0
			case contract.ActionCapture:
				p.beginShortcutCapture(wParam & 0xffff)
				return 0
			case contract.ActionSave:
				if err := p.save(); err != nil {
					uxSetText(p.hint, "保存失败："+err.Error())
				} else {
					// save() provides the precise message, including restart notices.
				}
				return 0
			case contract.ActionClose:
				procDestroyWindow.Call(hwnd)
				return 0
			case contract.ActionAdvanced:
				if err := shellOpenLocalFile(configPath()); err != nil {
					uxSetText(p.hint, "打开高级配置失败："+err.Error())
				}
				return 0
			case contract.ActionProbeASR:
				p.check("ASR", uxWinText(p.inputs[0]), uxWinText(p.inputs[2]), uxWinText(p.inputs[1]))
				return 0
			case contract.ActionProbeLLM:
				p.check("LLM", uxWinText(p.inputs[3]), uxWinText(p.inputs[5]), uxWinText(p.inputs[4]))
				return 0
			}
		}
	}
	v, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return v
}

func (p *uxSettings) save() error {
	if p.app.busy.Load() {
		return errors.New(ux.SaveWhileBusy)
	}
	var fields [6]string
	for i, h := range p.inputs {
		fields[i] = uxWinText(h)
	}
	cfg, err := uxUpdatedConfig(p.app.cfg, fields)
	if err != nil {
		return err
	}
	if p.capturingShortcut != 0 {
		return errors.New("请先完成按键录入，或按 Esc 取消")
	}
	newRewrite := strings.TrimSpace(uxWinText(p.rewriteShortcut))
	newDictation := strings.TrimSpace(uxWinText(p.shortcut))
	newRewrite, err = shortcut.ValidateRewrite(newRewrite, newDictation, cfg.HotkeyExit)
	if err != nil {
		return err
	}
	newDictation, err = shortcut.ValidateDictation(newDictation, newRewrite, cfg.HotkeyExit)
	if err != nil {
		return err
	}
	changedShortcut := cfg.HotkeyRecord != newRewrite || cfg.DictationHotkey != newDictation
	cfg.HotkeyRecord = newRewrite
	cfg.DictationHotkey = newDictation
	selected, _, _ := procSendMessage.Call(p.microphone, uxCBGetCurSel, 0, 0)
	if p.microphone != 0 && selected != ^uintptr(0) && selected < uintptr(len(p.microphoneIDs)) {
		cfg.AudioDeviceID = p.microphoneIDs[selected]
	}
	changedAudioDevice := cfg.AudioDeviceID != p.app.cfg.AudioDeviceID
	if err = saveConfig(cfg); err != nil {
		return err
	}
	p.app.cfg = cfg
	if changedShortcut || changedAudioDevice {
		uxSetText(p.hint, "设置已保存。快捷键和录音设备将在重启 VoiceRewrite 后生效。")
	} else {
		uxSetText(p.hint, "✓ 已保存，API 地址和模型立即生效")
	}
	p.app.logger.Print("settings updated: connection configuration saved (endpoint/key values redacted)")
	return nil
}

func (p *uxSettings) check(kind, base, key, model string) {
	if p == nil || p.window == 0 {
		return
	}
	field := "asr_base_url"
	if kind == "LLM" {
		field = "llm_base_url"
	}
	idx, ok := contract.ProbeSlot(field)
	if !ok || idx < 0 || idx >= len(p.probeStatus) {
		return
	}
	p.probeMu.Lock()
	p.probeSerial[idx]++
	serial := p.probeSerial[idx]
	p.probeMu.Unlock()
	uxSetText(p.probeStatus[idx], serviceprobe.StatusRunning)
	h := p.window
	go func() {
		result := serviceprobe.ProbeModel(context.Background(), kind, base, key, model)
		p.probeMu.Lock()
		if serial == p.probeSerial[idx] {
			p.probeResult[idx] = result
		}
		p.probeMu.Unlock()
		procPostMessage.Call(h, uxWMProbeDone, uintptr(idx), uintptr(serial))
	}()
}

func uxCreateControl(parent uintptr, class, text string, style uintptr, x, y, w, h int32, id uintptr, font uintptr) uintptr {
	cn := uxPtr(class)
	val := uxPtr(text)
	handle, _, _ := procCreateWindow.Call(0, uintptr(unsafe.Pointer(cn)), uintptr(unsafe.Pointer(val)), style,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, id, 0, 0)
	runtime.KeepAlive(cn)
	runtime.KeepAlive(val)
	if font != 0 {
		setFont(handle, font)
	}
	return handle
}

func uxWorkAreaAtCursor() desktop.WorkArea { return uxWorkAreaForPoint(currentCursorPoint()) }

func layoutSettingsFields(p *uxSettings, app *appState, makeOne func(cls, t string, style uintptr, x, y, w, hh int32, id uintptr, head bool) uintptr, stat, edit, btn uintptr) error {
	for pageIndex, page := range contract.Pages() {
		start := len(p.controls)
		row := 0
		for _, control := range contract.PageControls(page.ID, contract.Windows) {
			if err := placeSettingsControl(p, app, makeOne, stat, edit, btn, control, row); err != nil {
				return err
			}
			if control.Stacked {
				row++
			}
		}
		for _, created := range p.controls[start:] {
			p.pages[pageIndex] = append(p.pages[pageIndex], created.hwnd)
		}
	}
	for _, control := range contract.PageControls("save", contract.Windows) {
		if err := placeSettingsControl(p, app, makeOne, stat, edit, btn, control, 0); err != nil {
			return err
		}
	}
	return nil
}

func placeSettingsControl(p *uxSettings, app *appState, makeOne func(cls, t string, style uintptr, x, y, w, hh int32, id uintptr, head bool) uintptr, stat, edit, btn uintptr, control contract.Control, row int) error {
	label := settingsform.FieldLabel(row)
	switch control.ID {
	case "asr_base_url":
		placeLabeledEdit(p, makeOne, stat, edit, control, row, 0, app.cfg.ASRBaseURL, uxASRURLInput, false)
	case "asr_model":
		placeLabeledEdit(p, makeOne, stat, edit, control, row, 1, app.cfg.ASRModel, uxASRModelInput, false)
	case "asr_api_key":
		placeLabeledEdit(p, makeOne, stat, edit, control, row, 2, app.cfg.ASRAPIKey, uxASRKeyInput, true)
	case "probe_asr_status":
		if err := placeProbeStatus(p, makeOne, stat, "asr_base_url"); err != nil {
			return err
		}
	case "probe_asr":
		placeProbeButton(makeOne, btn, control, contract.ProbeASRButton)
	case "llm_base_url":
		placeLabeledEdit(p, makeOne, stat, edit, control, row, 3, app.cfg.LLMBaseURL, uxLLMURLInput, false)
	case "text_model":
		placeLabeledEdit(p, makeOne, stat, edit, control, row, 4, app.cfg.TextModel, uxLLMModelInput, false)
	case "llm_api_key":
		placeLabeledEdit(p, makeOne, stat, edit, control, row, 5, app.cfg.LLMAPIKey, uxLLMKeyInput, true)
	case "probe_llm_status":
		if err := placeProbeStatus(p, makeOne, stat, "llm_base_url"); err != nil {
			return err
		}
	case "probe_llm":
		placeProbeButton(makeOne, btn, control, contract.ProbeLLMButton)
	case "rewrite_shortcut":
		p.rewriteShortcut = placeShortcutEdit(makeOne, stat, edit, control, row, app.cfg.HotkeyRecord, uxRewriteInput)
	case "capture_rewrite":
		button, err := placeCaptureButton(makeOne, btn, control, "rewrite_shortcut", contract.CaptureRewrite)
		if err != nil {
			return err
		}
		p.captureButtons[0] = button
	case "dictation_shortcut":
		p.shortcut = placeShortcutEdit(makeOne, stat, edit, control, row, app.cfg.DictationHotkey, uxShortcutInput)
	case "capture_dictation":
		button, err := placeCaptureButton(makeOne, btn, control, "dictation_shortcut", contract.CaptureDictation)
		if err != nil {
			return err
		}
		p.captureButtons[1] = button
	case "microphone":
		frame := settingsform.FieldFrame(row)
		makeOne("STATIC", control.Label, stat, label.X, label.Y, label.W, label.H, 0, false)
		p.microphone = makeOne("COMBOBOX", "", uxWSChild|uxWSVisible|uxWSTabStop|uxCBSDropDownList|WS_VSCROLL, frame.X+5, frame.Y+4, frame.W-10, 190, uxMicrophoneInput, false)
		p.populateMicrophones(app.cfg.AudioDeviceID)
		resultStyleNativeEdit(p.microphone, p.palette.dark)
	case "input_note_windows":
		info := makeOne("STATIC", control.Label, stat, 226, 345, 565, 44, 0, false)
		setFont(info, p.mutedFont)
	case "save":
		makeOne("BUTTON", control.Label, btn, 220, 418, 124, 34, contract.SaveButton, false)
	case "advanced":
		makeOne("BUTTON", control.Label, btn, 358, 418, 112, 34, contract.AdvancedButton, false)
	case "close":
		makeOne("BUTTON", control.Label, btn, 710, 418, 94, 34, contract.CloseButton, false)
	default:
		return fmt.Errorf("设置页缺少控件布局: %s", control.ID)
	}
	return nil
}

func placeLabeledEdit(p *uxSettings, makeOne func(cls, t string, style uintptr, x, y, w, hh int32, id uintptr, head bool) uintptr, stat, edit uintptr, control contract.Control, row, index int, value string, id uintptr, secret bool) {
	label := settingsform.FieldLabel(row)
	field := settingsform.FieldEdit(row)
	style := edit
	if secret {
		style |= uxESPassword
	}
	makeOne("STATIC", control.Label, stat, label.X, label.Y, label.W, label.H, 0, false)
	hwnd := makeOne("EDIT", value, style, field.X, field.Y, field.W, field.H, id, false)
	p.inputs[index] = hwnd
	if slot, ok := contract.ProbeSlot(control.ID); ok && slot >= 0 && slot < len(p.probeStatus) {
		p.probeEdits[hwnd] = slot
	}
}

func placeProbeStatus(p *uxSettings, makeOne func(cls, t string, style uintptr, x, y, w, hh int32, id uintptr, head bool) uintptr, stat uintptr, fieldID string) error {
	slot, ok := contract.ProbeSlot(fieldID)
	if !ok || slot < 0 || slot >= len(p.probeStatus) {
		return fmt.Errorf("设置页缺少连接检测: %s", fieldID)
	}
	p.probeStatus[slot] = makeOne("STATIC", serviceprobe.StatusIdle, stat, 352, 324, 435, 48, 0, false)
	return nil
}

func placeProbeButton(makeOne func(cls, t string, style uintptr, x, y, w, hh int32, id uintptr, head bool) uintptr, btn uintptr, control contract.Control, id uintptr) {
	makeOne("BUTTON", control.Label, btn, 696, 130, 92, 30, id, false)
}

func placeShortcutEdit(makeOne func(cls, t string, style uintptr, x, y, w, hh int32, id uintptr, head bool) uintptr, stat, edit uintptr, control contract.Control, row int, value string, id uintptr) uintptr {
	label := settingsform.FieldLabel(row)
	shortcut := settingsform.ShortcutEdit()
	shortcut.Y += settingsform.RowStep * int32(row)
	makeOne("STATIC", control.Label, stat, label.X, label.Y, label.W, label.H, 0, false)
	return makeOne("EDIT", value, edit|0x0800, shortcut.X, shortcut.Y, shortcut.W, shortcut.H, id, false)
}

func placeCaptureButton(makeOne func(cls, t string, style uintptr, x, y, w, hh int32, id uintptr, head bool) uintptr, btn uintptr, control contract.Control, shortcutID string, id uintptr) (uintptr, error) {
	row, ok := stackedSettingsRow(shortcutID)
	if !ok {
		return 0, fmt.Errorf("设置页缺少控件布局: %s", shortcutID)
	}
	frame := settingsform.FieldFrame(row)
	return makeOne("BUTTON", control.Label, btn, 593, frame.Y, 194, 34, id, false), nil
}

func stackedSettingsRow(id string) (int, bool) {
	for i, control := range contract.Stacked("input", contract.Windows) {
		if control.ID == id {
			return i, true
		}
	}
	return 0, false
}

func showNativeSettings(app *appState) error {
	restoreDPI := handyAwareWindowThread()
	defer restoreDPI()
	if uxActiveSettings != nil && uxActiveSettings.window != 0 {
		procShowWindow.Call(uxActiveSettings.window, SW_SHOW)
		procSetForegroundWindow.Call(uxActiveSettings.window)
		return nil
	}
	className := uxPtr("VoiceRewrite.Settings")
	title := uxPtr("VoiceRewrite · 连接设置")
	module, _, _ := procGetModuleHandle.Call(0)
	klass := trayWindowClass{Size: uint32(unsafe.Sizeof(trayWindowClass{})), WndProc: uxWindowCallback, Instance: module, ClassName: className}
	atom, _, regErr := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&klass)))
	if atom == 0 && regErr != syscall.Errno(1410) {
		return fmt.Errorf("无法注册设置窗口: %w", regErr)
	}
	// 96-DPI logical layout; all children use the same scale as the parent.
	scale := handyMonitorScale()
	if scale < 1 {
		scale = 1
	}
	sc := func(v int32) uintptr { return uintptr(int32(float64(v)*scale + 0.5)) }
	work := uxWorkAreaAtCursor()
	winW, winH := desktop.Fit(int32(sc(860)), int32(sc(530)), work, 12)
	// The recording pill stays near the bottom of the work area. Settings opens in the middle.
	x, y := desktop.Center(winW, winH, work)
	h, _, createErr := procCreateWindow.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		uxWSCaption|uxWSSysMenu|uxWSMinimizeBox|uxWSThickFrame|WS_VSCROLL|0x00100000, uintptr(x), uintptr(y), uintptr(winW), uintptr(winH), 0, 0, module, 0)
	if h == 0 {
		return fmt.Errorf("创建设置窗口失败: %w", createErr)
	}
	p := &uxSettings{window: h, app: app, scale: scale, focusRow: -1, cardStatics: make(map[uintptr]bool), probeEdits: make(map[uintptr]int), palette: resultCardPalette()}
	uxActiveSettings = p
	p.background, _, _ = procCreateSolidBrush.Call(p.palette.color(p.palette.background))
	p.cardBrush, _, _ = procCreateSolidBrush.Call(p.palette.color(p.palette.surface))
	p.editBrush, _, _ = procCreateSolidBrush.Call(p.palette.color(p.palette.field))
	applyWindowPalette(h, p.palette)
	if renderer, renderErr := newResultRenderer(h, scale, "设置", p.palette.dark, false); renderErr == nil {
		p.renderer = renderer
	} else if app != nil && app.logger != nil {
		app.logger.Printf("settings Direct2D unavailable; using GDI: %v", renderErr)
	}
	// Match the DirectWrite Chinese UI face; Segoe UI Variable can fall back
	// to a serif CJK face in Win32 EDIT/STATIC controls on some systems.
	fontName := uxPtr("Microsoft YaHei UI")
	p.font, _, _ = procCreateFont.Call(sc(-16), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)))
	p.titleFont, _, _ = procCreateFont.Call(sc(-24), 0, 0, 0, 650, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)))
	p.sectionFont, _, _ = procCreateFont.Call(sc(-18), 0, 0, 0, 600, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)))
	p.mutedFont, _, _ = procCreateFont.Call(sc(-14), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)))
	makeOne := func(cls, t string, style uintptr, x, y, w, hh int32, id uintptr, head bool) uintptr {
		useFont := p.font
		if head {
			useFont = p.sectionFont
		}
		control := uxCreateControl(h, cls, t, style, int32(sc(x)), int32(sc(y)), int32(sc(w)), int32(sc(hh)), id, useFont)
		if control == 0 {
			return 0
		}
		if cls == "EDIT" {
			procSendMessage.Call(control, uxEMSetMargins, 3, uintptr(8|(8<<16)))
			resultStyleNativeEdit(control, p.palette.dark)
		}
		// Flat, unboxed settings rows have one continuous background.
		p.controls = append(p.controls, uxControl{
			hwnd: control, x: x, y: y, w: w, h: hh,
			stretch: false,
			right:   false,
			id:      id,
		})
		return control
	}
	stat := uintptr(uxWSChild | uxWSVisible)
	// Borderless Win32 EDIT + Direct2D rounded backplate: native selection, IME,
	// password masking and Ctrl+C remain available.
	edit := uintptr(uxWSChild | uxWSVisible | uxWSTabStop | uxESAutoScroll)
	btn := uintptr(uxWSChild | uxWSVisible | uxWSTabStop | uxBSOwnerDraw)
	// Native sidebar navigation, one focused form page at a time.
	pages := contract.Pages()
	if len(pages) != len(p.navButtons) {
		return fmt.Errorf("设置导航与页面数量不一致")
	}
	for i, page := range pages {
		p.navButtons[i] = makeOne("BUTTON", page.Title, btn, 12, int32(110+i*41), 170, 35, contract.NavSpeech+uintptr(i), false)
	}
	head := makeOne("STATIC", "设置", uxWSChild, 0, 0, 1, 1, 0, false)
	setFont(head, p.titleFont)
	if err := layoutSettingsFields(p, app, makeOne, stat, edit, btn); err != nil {
		return err
	}
	p.hint = makeOne("STATIC", "", stat, 223, 457, 560, 22, 0, false)
	setFont(p.hint, p.mutedFont)
	setFont(p.probeStatus[0], p.mutedFont)
	setFont(p.probeStatus[1], p.mutedFont)
	p.switchPage(0)
	p.layout()
	// Windows 11 DWM-rounded frame and non-activating status pill are separate.
	rounding := uint32(2)
	procDwmSetWindowAttribute.Call(h, DWMWA_WINDOW_CORNER_PREFERENCE, uintptr(unsafe.Pointer(&rounding)), 4)
	procShowWindow.Call(h, SW_SHOW)
	procSetForegroundWindow.Call(h)
	return nil
}
func closeNativeSettings() {
	if uxActiveSettings != nil && uxActiveSettings.window != 0 {
		procDestroyWindow.Call(uxActiveSettings.window)
	}
}
