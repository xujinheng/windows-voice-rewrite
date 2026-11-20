//go:build windows

package main

import (
	"unsafe"

	"github.com/xujinheng/windows-voice-rewrite/core/internal/contract"
	"github.com/xujinheng/windows-voice-rewrite/core/settingsform"
)

// These are deliberately *not* raised cards. A quiet left navigation rail,
// understated dividers and one focused settings pane follow Raycast/Linear's
// navigational hierarchy, while the application's identity stays original.
func settingsPage(index int) contract.Page {
	pages := contract.Pages()
	if index < 0 || index >= len(pages) {
		return pages[0]
	}
	return pages[index]
}

func (d *resultRenderer) drawSettings(page int, scrollX, scrollY int32, focusRow int) bool {
	if d == nil {
		return false
	}
	if d.target == 0 {
		if err := d.createTarget(); err != nil {
			return false
		}
	}
	var rc rect32
	procGetClientRect.Call(d.hwnd, uintptr(unsafe.Pointer(&rc)))
	w, h := float32(rc.Right)/d.scale, float32(rc.Bottom)/d.scale
	transform := resultD2DMatrix{M11: d.scale, M22: d.scale}
	resultCOM(d.target, 30, uintptr(unsafe.Pointer(&transform)))
	resultCOM(d.target, 48)
	bg := resultColor(d.colors[0])
	resultCOM(d.target, 47, uintptr(unsafe.Pointer(&bg)))
	// Persistent left navigation is intentionally less contrasty than content.
	rail := settingsform.RailFill(0)
	d.fill(float32(rail.X), float32(rail.Y), float32(rail.W), h, 1)
	divider := settingsform.RailDivider(0)
	d.fill(float32(divider.X), float32(divider.Y), float32(divider.W), h, 6)
	brand := settingsform.DirectBrand()
	d.text(settingsform.BrandTitle, float32(brand.X), float32(brand.Y), float32(brand.W), float32(brand.H), 3, 4)
	category := settingsform.DirectCategory()
	d.text(settingsform.RailCategory, float32(category.X), float32(category.Y), float32(category.W), float32(category.H), 0, 5)
	rule := settingsform.DirectRailRule()
	d.fill(float32(rule.X), float32(rule.Y), float32(rule.W), float32(rule.H), 6)
	// No decorative footer copy: navigation is the only purpose of the rail.
	// Main pane. No brand banner, stacked white cards or redundant headings.
	shown := settingsPage(page)
	titleX, titleY, titleCut, titleH := settingsform.DirectTitle()
	d.text(shown.Title, float32(titleX), float32(titleY), w-float32(titleCut), float32(titleH), 2, 4)
	subX, subY, subCut, subH := settingsform.DirectSubtitle()
	d.text(shown.Subtitle, float32(subX), float32(subY), w-float32(subCut), float32(subH), 0, 5)
	ruleX, ruleY, ruleCut, ruleH := settingsform.DirectSectionRule()
	d.fill(float32(ruleX), float32(ruleY), w-float32(ruleCut), float32(ruleH), 6)
	section := settingsform.DirectSection()
	d.text(shown.Section, float32(section.X), float32(section.Y), float32(section.W), float32(section.H), 3, 4)
	// Shared geometry puts the real EDIT fully inside the painted rounded
	// field, including its left edge. Fields are now 34px tall, with a compact
	// 54px row pitch; native text has its own 24px vertically centered HWND.
	for i, frame := range settingsform.PaintedFrames(shown.ID) {
		x := float32(frame.X - scrollX)
		y := float32(frame.Y - scrollY)
		outline := 6
		if i == focusRow {
			outline = 7
		}
		d.round(x, y, float32(frame.W), float32(frame.H), float32(settingsform.DirectFieldRadius), 2)
		d.outline(x, y, float32(frame.W), float32(frame.H), float32(settingsform.DirectFieldRadius), outline)
	}
	footX, footCut, footW, footH := settingsform.DirectFooterRule()
	d.fill(float32(footX), h-float32(footCut), w-float32(footW), float32(footH), 6)
	hr := resultCOM(d.target, 49, 0, 0)
	if uint32(hr) == 0x8899000c {
		d.disposeTarget()
	}
	return int32(hr) >= 0
}

// GDI paints the same overall navigation and main hierarchy if Direct2D is
// unavailable, without sacrificing Win32 fields or keyboard access.
func (p *uxSettings) drawSettingsFallback(hdc uintptr) {
	sc := func(n int32) int32 { return int32(float64(n)*p.scale + 0.5) }
	var rc rect32
	procGetClientRect.Call(p.window, uintptr(unsafe.Pointer(&rc)))
	rail := settingsform.RailFill(0)
	side := rect32{0, 0, sc(rail.W), rc.Bottom}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&side)), p.cardBrush)
	divider := settingsform.RailDivider(0)
	line := rect32{sc(divider.X), 0, sc(divider.X + divider.W), rc.Bottom}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&line)), p.editBrush)
	page := settingsPage(p.page)
	for i, frame := range settingsform.PaintedFrames(page.ID) {
		outline := p.palette.color(p.palette.border)
		if p.focusRow == i {
			outline = p.palette.color(p.palette.primary)
		}
		uiRoundSurface(hdc, rect32{sc(frame.X - p.scrollX), sc(frame.Y - p.scrollY),
			sc(frame.X + frame.W - p.scrollX), sc(frame.Y + frame.H - p.scrollY)}, sc(settingsform.FallbackFieldRadius), p.palette.color(p.palette.field), outline)
	}
	procSetBkMode.Call(hdc, TRANSPARENT)
	procSetTextColor.Call(hdc, p.palette.color(p.palette.text))
	head, _, _ := procSelectObject.Call(hdc, p.titleFont)
	title := uxPtr(page.Title)
	titleEdges := settingsform.FallbackTitle()
	bounds := rect32{sc(titleEdges.Left), sc(titleEdges.Top), rc.Right - sc(titleEdges.Right), sc(titleEdges.Bottom)}
	procDrawText.Call(hdc, uintptr(unsafe.Pointer(title)), ^uintptr(0), uintptr(unsafe.Pointer(&bounds)), 0)
	procSelectObject.Call(hdc, head)
	procSetTextColor.Call(hdc, p.palette.color(p.palette.muted))
	sub := uxPtr(page.Subtitle)
	subEdges := settingsform.FallbackSubtitle()
	bounds = rect32{sc(subEdges.Left), sc(subEdges.Top), rc.Right - sc(subEdges.Right), sc(subEdges.Bottom)}
	procDrawText.Call(hdc, uintptr(unsafe.Pointer(sub)), ^uintptr(0), uintptr(unsafe.Pointer(&bounds)), 0)
}

// Owner-drawn native sidebar BUTTON: focusable, keyboard-activatable, no
// browser renderer. Only the selected item receives a tinted background.
func (p *uxSettings) drawSidebarItem(item *drawItemStruct, index int) bool {
	if p == nil || item == nil || index < 0 || index >= len(contract.Pages()) {
		return false
	}
	r := item.Rect
	// Inactive items are transparent relative to the sidebar, not three
	// separately filled cards.
	sideRGB := settingsform.RailColor(p.palette.dark)
	side, _, _ := procCreateSolidBrush.Call(p.palette.color(sideRGB))
	if side != 0 {
		procFillRect.Call(item.Hdc, uintptr(unsafe.Pointer(&r)), side)
		procDeleteObject.Call(side)
	}
	if index == p.page {
		uiRoundSurface(item.Hdc, r, settingsform.NavRadius, p.palette.color(p.palette.secondary), p.palette.color(p.palette.secondary))
		accent, _, _ := procCreateSolidBrush.Call(p.palette.color(p.palette.primary))
		if accent != 0 {
			markEdges := settingsform.NavAccent(settingsform.Edges{Left: r.Left, Top: r.Top, Right: r.Right, Bottom: r.Bottom})
			mark := rect32{markEdges.Left, markEdges.Top, markEdges.Right, markEdges.Bottom}
			procFillRect.Call(item.Hdc, uintptr(unsafe.Pointer(&mark)), accent)
			procDeleteObject.Call(accent)
		}
	}

	procSetBkMode.Call(item.Hdc, TRANSPARENT)
	ink := p.palette.muted
	if index == p.page {
		ink = p.palette.text
	}
	procSetTextColor.Call(item.Hdc, p.palette.color(ink))
	if p.font != 0 {
		old, _, _ := procSelectObject.Call(item.Hdc, p.font)
		defer procSelectObject.Call(item.Hdc, old)
	}
	label := uxPtr(contract.Pages()[index].Title)
	r.Left = settingsform.NavLabelLeft(r.Left)
	procDrawText.Call(item.Hdc, uintptr(unsafe.Pointer(label)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), DT_VCENTER|DT_SINGLELINE)
	if item.ItemState&0x0010 != 0 {
		focusEdges := settingsform.NavFocus(settingsform.Edges{Left: item.Rect.Left, Top: item.Rect.Top, Right: item.Rect.Right, Bottom: item.Rect.Bottom})
		focus := rect32{focusEdges.Left, focusEdges.Top, focusEdges.Right, focusEdges.Bottom}
		user32.NewProc("DrawFocusRect").Call(item.Hdc, uintptr(unsafe.Pointer(&focus)))
	}
	return true
}
