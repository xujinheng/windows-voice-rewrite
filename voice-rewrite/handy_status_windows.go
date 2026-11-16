//go:build windows

package main

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"voicerewrite/internal/core/desktop"
	"voicerewrite/internal/core/indicator"
)

// GDI+ recording status overlay.
// Visual reference: Hyroniem/handy-remote src/overlay/RecordingOverlay.{tsx,css}
// (MIT, copyright 2025 CJ Pais). All controls below are implemented in Go and
// the Windows system GDI+ renderer; Tauri and WebView2 are not required.
const (
	handyPulseTimer          uintptr = 7331
	WM_PAINT                         = 0x000F
	WM_LBUTTONUP                     = 0x0202
	WS_EX_NOACTIVATE                 = 0x08000000
	WS_EX_LAYERED                    = 0x00080000
	MONITOR_DEFAULTTONEAREST         = 2

	handyBodyTop               = 5
	handyBodyHeight            = 40
	handyPillInset             = 5
	handyPixelFormat32bppPARGB = 0x000E200B
	handyULW_ALPHA             = 0x00000002
)

type handyStatus struct {
	window        uintptr
	phase         indicator.Phase
	mode          string
	owner         uint64
	mic           *audioRecorder
	pending       bool // side-button preview, before the 250ms hold decision
	started       time.Time
	changed       time.Time
	tick          uint32
	smoothedLevel float64
	closeHovered  bool
	x, y          int32
	monitor       uintptr
	scale         float64
}

var activeHandyStatus *handyStatus
var activeHandyAppWindow uintptr

var (
	gdiplusHandy                 = syscall.NewLazyDLL("gdiplus.dll")
	gpStartup                    = gdiplusHandy.NewProc("GdiplusStartup")
	gpShutdown                   = gdiplusHandy.NewProc("GdiplusShutdown")
	gpBitmapFromScan             = gdiplusHandy.NewProc("GdipCreateBitmapFromScan0")
	gpGetGraphics                = gdiplusHandy.NewProc("GdipGetImageGraphicsContext")
	gpDeleteGraphics             = gdiplusHandy.NewProc("GdipDeleteGraphics")
	gpDisposeImage               = gdiplusHandy.NewProc("GdipDisposeImage")
	gpGraphicsClear              = gdiplusHandy.NewProc("GdipGraphicsClear")
	gpSetSmoothingMode           = gdiplusHandy.NewProc("GdipSetSmoothingMode")
	gpSetTextRenderingHint       = gdiplusHandy.NewProc("GdipSetTextRenderingHint")
	gpCreateSolidFill            = gdiplusHandy.NewProc("GdipCreateSolidFill")
	gpDeleteBrush                = gdiplusHandy.NewProc("GdipDeleteBrush")
	gpFillEllipseI               = gdiplusHandy.NewProc("GdipFillEllipseI")
	gpFillRectangleI             = gdiplusHandy.NewProc("GdipFillRectangleI")
	gpFillPolygonI               = gdiplusHandy.NewProc("GdipFillPolygonI")
	gpDrawString                 = gdiplusHandy.NewProc("GdipDrawString")
	gpStringFormatCreate         = gdiplusHandy.NewProc("GdipCreateStringFormat")
	gpStringFormatDelete         = gdiplusHandy.NewProc("GdipDeleteStringFormat")
	gpStringFormatLineAlign      = gdiplusHandy.NewProc("GdipSetStringFormatLineAlign")
	gpCreateFontFromDC           = gdiplusHandy.NewProc("GdipCreateFontFromDC")
	gpDeleteFont                 = gdiplusHandy.NewProc("GdipDeleteFont")
	procCreateCompatibleDCHandy  = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDCHandy            = gdi32.NewProc("DeleteDC")
	procCreateDIBSectionHandy    = gdi32.NewProc("CreateDIBSection")
	procGetDCScreenHandy         = user32.NewProc("GetDC")
	procReleaseDCScreenHandy     = user32.NewProc("ReleaseDC")
	procUpdateLayeredHandy       = user32.NewProc("UpdateLayeredWindow")
	procSetThreadDpiContextHandy = user32.NewProc("SetThreadDpiAwarenessContext")
	shcoreHandy                  = syscall.NewLazyDLL("shcore.dll")
	procGetDpiForMonitorHandy    = shcoreHandy.NewProc("GetDpiForMonitor")
	procBeginPaintHandy          = user32.NewProc("BeginPaint")
	procEndPaintHandy            = user32.NewProc("EndPaint")
	procInvalidateRectHandy      = user32.NewProc("InvalidateRect")
	procMonitorFromPointHandy    = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoHandy      = user32.NewProc("GetMonitorInfoW")
	handyStatusWndProcCallback   = syscall.NewCallback(handyStatusWndProc)
	handyGdiInit                 sync.Once
	handyGdiInitErr              error
)

type handyMonitorInfo struct {
	Size          uint32
	Monitor, Work rect32
	Flags         uint32
}
type handyPaintStruct struct {
	HDC       uintptr
	Erase     int32
	PaintRect rect32
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}
type handyGdiplusStartupInput struct {
	Version                  uint32
	_                        uint32
	Callback                 uintptr
	SuppressBackgroundThread int32
	SuppressExternalCodecs   int32
}
type handyBitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}
type handyBitmapInfo struct {
	Header handyBitmapInfoHeader
	Colors [1]uint32
}
type handyPoint struct{ X, Y int32 }
type handySize struct{ CX, CY int32 }
type handyBlend struct{ Op, Flags, Alpha, Format uint8 }
type handyRectF struct{ X, Y, Width, Height float32 }

func handyStatusWndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch uint32(message) {
	case WM_ERASEBKGND:
		return 1
	case WM_PAINT:
		var ps handyPaintStruct
		hdc, _, _ := procBeginPaintHandy.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			procEndPaintHandy.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		}
		return 0
	case WM_TIMER:
		if wParam == handyPulseTimer && activeHandyStatus != nil && activeHandyStatus.window == hwnd {
			s := activeHandyStatus
			if indicator.Terminal(s.phase) && time.Since(s.changed) > indicator.TerminalDuration {
				closeHandyStatus()
				return 0
			}
			s.tick++
			drawHandyStatus(s)
			return 0
		}
	case 0x0200: // WM_MOUSEMOVE
		if activeHandyStatus != nil && activeHandyStatus.window == hwnd {
			s := activeHandyStatus
			x := int(int16(lParam & 0xffff))
			hover := indicator.CancelHit(int(math.Floor(float64(x)/s.scale)), indicator.Width(s.phase))
			if hover != s.closeHovered {
				s.closeHovered = hover
				drawHandyStatus(s)
			}
			if hover {
				var tme = struct {
					Size, Flags     uint32
					Hwnd, HoverTime uintptr
				}{Size: uint32(unsafe.Sizeof(struct {
					Size, Flags     uint32
					Hwnd, HoverTime uintptr
				}{})), Flags: 2, Hwnd: hwnd}
				user32.NewProc("TrackMouseEvent").Call(uintptr(unsafe.Pointer(&tme)))
			}
		}
	case 0x02A3: // WM_MOUSELEAVE
		if activeHandyStatus != nil && activeHandyStatus.window == hwnd {
			activeHandyStatus.closeHovered = false
			drawHandyStatus(activeHandyStatus)
		}
	case WM_LBUTTONUP:
		if activeHandyStatus != nil && activeHandyStatus.window == hwnd {
			x := int(int16(lParam & 0xffff))
			// WM_LBUTTONUP is in *physical client pixels* under the PMv2
			// context. The hitbox geometry is defined in logical units.
			logicalX := int(math.Floor(float64(x) / activeHandyStatus.scale))
			if indicator.CancelHit(logicalX, indicator.Width(activeHandyStatus.phase)) {
				procPostMessage.Call(activeHandyAppWindow, WM_STATUS_CANCEL, uintptr(activeHandyStatus.owner), 0)
				return 0
			}
		}
	case WM_CLOSE:
		if activeHandyStatus != nil && activeHandyStatus.window == hwnd {
			procPostMessage.Call(activeHandyAppWindow, WM_STATUS_CANCEL, uintptr(activeHandyStatus.owner), 0)
		}
		return 0
	}
	v, _, _ := procDefWindowProc.Call(hwnd, message, wParam, lParam)
	return v
}

func initHandyGDI() error {
	handyGdiInit.Do(func() {
		var token uintptr
		input := handyGdiplusStartupInput{Version: 1}
		status, _, _ := gpStartup.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&input)), 0)
		if status != 0 || token == 0 {
			handyGdiInitErr = fmt.Errorf("GDI+ 初始化失败: status=%d", status)
		}
		// GDI+ stays loaded until process exit, so its state cannot race a
		// WM_TIMER with a teardown; Windows releases it at process exit.
	})
	return handyGdiInitErr
}

type handyPainter struct {
	graphics, dc uintptr
	scale        float64
}

func (p handyPainter) fill(argb uint32, x, y, w, h int, ellipse bool) {
	x = int(math.Round(float64(x) * p.scale))
	y = int(math.Round(float64(y) * p.scale))
	w = int(math.Round(float64(w) * p.scale))
	h = int(math.Round(float64(h) * p.scale))
	if w <= 0 || h <= 0 {
		return
	}
	var brush uintptr
	if ret, _, _ := gpCreateSolidFill.Call(uintptr(argb), uintptr(unsafe.Pointer(&brush))); ret != 0 || brush == 0 {
		return
	}
	if ellipse {
		gpFillEllipseI.Call(p.graphics, brush, uintptr(x), uintptr(y), uintptr(w), uintptr(h))
	} else {
		gpFillRectangleI.Call(p.graphics, brush, uintptr(x), uintptr(y), uintptr(w), uintptr(h))
	}
	gpDeleteBrush.Call(brush)
}
func (p handyPainter) pill(color uint32, x, y, w, h int) {
	// 4 rounded quadrants + 2 rects, entirely in GDI+ anti-aliased geometry.
	if w <= 0 || h <= 0 {
		return
	}
	d := h
	if d > w {
		d = w
	}
	p.fill(color, x, y, d, d, true)
	p.fill(color, x+w-d, y, d, d, true)
	if w > d {
		p.fill(color, x+d/2, y, w-d, h, false)
	}
}
func (p handyPainter) roundBar(color uint32, x, y, w, h int) {
	if h < w {
		p.fill(color, x, y, w, h, true)
		return
	}
	p.fill(color, x, y, w, w, true)
	p.fill(color, x, y+h-w, w, w, true)
	if h > w {
		p.fill(color, x, y+w/2, w, h-w, false)
	}
}

// Draw a crisp checkmark with GDI+ polygons; using a text glyph produced a
// missing wedge on some CJK fonts/DPI settings.
func (p handyPainter) checkGlyph(cx, cy int) {
	type vertex struct{ X, Y int32 }
	shape := [6]vertex{{-6, 0}, {-4, -2}, {-1, 1}, {5, -6}, {7, -4}, {-1, 5}}
	var brush uintptr
	if ret, _, _ := gpCreateSolidFill.Call(uintptr(indicator.CheckGlyph.Opaque()), uintptr(unsafe.Pointer(&brush))); ret != 0 || brush == 0 {
		return
	}
	defer gpDeleteBrush.Call(brush)
	var vertices [6]vertex
	for i, v := range shape {
		vertices[i] = vertex{int32(math.Round(float64(cx+int(v.X)) * p.scale)), int32(math.Round(float64(cy+int(v.Y)) * p.scale))}
	}
	gpFillPolygonI.Call(p.graphics, brush, uintptr(unsafe.Pointer(&vertices[0])), uintptr(len(vertices)), 0)
}

// Use a polygon close icon that scales with the window DPI.
func (p handyPainter) closeGlyph(cx, cy int) {
	type vertex struct{ X, Y int32 }
	polygons := [2][6]vertex{
		{{-6, -5}, {-4, -6}, {6, 4}, {6, 6}, {4, 6}, {-6, -4}},
		{{4, -6}, {6, -5}, {-4, 6}, {-6, 6}, {-6, 4}, {4, -6}},
	}
	var brush uintptr
	if ret, _, _ := gpCreateSolidFill.Call(uintptr(indicator.CloseGlyph.Opaque()), uintptr(unsafe.Pointer(&brush))); ret != 0 || brush == 0 {
		return
	}
	defer gpDeleteBrush.Call(brush)
	for _, shape := range polygons {
		var vertices [6]vertex
		for i, v := range shape {
			vertices[i] = vertex{int32(float64(cx+int(v.X)) * p.scale), int32(float64(cy+int(v.Y)) * p.scale)}
		}
		gpFillPolygonI.Call(p.graphics, brush, uintptr(unsafe.Pointer(&vertices[0])), uintptr(len(vertices)), 0)
	}
}

func (p handyPainter) text(text string, x, y, w, h, size, weight int, color uint32) {
	name, _ := syscall.UTF16PtrFromString("Microsoft YaHei UI")
	font, _, _ := procCreateFont.Call(uintptr(int32(-int(math.Round(float64(size)*p.scale)))), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 4, 0, uintptr(unsafe.Pointer(name)))
	runtime.KeepAlive(name)
	if font == 0 {
		return
	}
	old, _, _ := procSelectObject.Call(p.dc, font)
	var gpFont uintptr
	status, _, _ := gpCreateFontFromDC.Call(p.dc, uintptr(unsafe.Pointer(&gpFont)))
	if status == 0 && gpFont != 0 {
		u16, err := syscall.UTF16FromString(text)
		if err == nil {
			var brush uintptr
			gpCreateSolidFill.Call(uintptr(color), uintptr(unsafe.Pointer(&brush)))
			if brush != 0 {
				// DrawString has an anti-aliased glyph raster and supports CJK fallback.
				rect := handyRectF{X: float32(float64(x) * p.scale), Y: float32(float64(y) * p.scale), Width: float32(float64(w) * p.scale), Height: float32(float64(h) * p.scale)}
				var format uintptr
				if result, _, _ := gpStringFormatCreate.Call(0, 0, uintptr(unsafe.Pointer(&format))); result == 0 && format != 0 {
					// Middle vertically, left horizontally. Avoid oversized
					// label baselines drifting down at 150/200 percent DPI.
					gpStringFormatLineAlign.Call(format, 1)
				}
				gpDrawString.Call(p.graphics, uintptr(unsafe.Pointer(&u16[0])), uintptr(len(u16)-1), gpFont, uintptr(unsafe.Pointer(&rect)), format, brush)
				if format != 0 {
					gpStringFormatDelete.Call(format)
				}
				runtime.KeepAlive(u16)
				gpDeleteBrush.Call(brush)
			}
		}
		gpDeleteFont.Call(gpFont)
	}
	procSelectObject.Call(p.dc, old)
	procDeleteObject.Call(font)
}

// Paints a premultiplied alpha top-down 32-bit DIB and atomically presents it
// through UpdateLayeredWindow (per-pixel antialiasing, no SetWindowRgn jaggies).
// Runs on the Win32 UI thread; the async ASR/LLM workers only post status events.
func drawHandyStatus(s *handyStatus) {
	restore := handyAwareWindowThread()
	defer restore()
	if s == nil || s.window == 0 || initHandyGDI() != nil {
		return
	}
	logicalWidth := indicator.Width(s.phase)
	width, height := indicator.PhysicalSize(logicalWidth, indicator.Height, s.scale)
	dc, _, _ := procCreateCompatibleDCHandy.Call(0)
	if dc == 0 {
		return
	}
	defer procDeleteDCHandy.Call(dc)
	bmi := handyBitmapInfo{Header: handyBitmapInfoHeader{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}}
	var bits uintptr
	bitmap, _, _ := procCreateDIBSectionHandy.Call(dc, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == 0 {
		return
	}
	old, _, _ := procSelectObject.Call(dc, bitmap)
	defer func() { procSelectObject.Call(dc, old); procDeleteObject.Call(bitmap) }()
	var gpImage uintptr
	if ret, _, _ := gpBitmapFromScan.Call(uintptr(width), uintptr(height), uintptr(width*4), handyPixelFormat32bppPARGB, bits, uintptr(unsafe.Pointer(&gpImage))); ret != 0 || gpImage == 0 {
		return
	}
	defer gpDisposeImage.Call(gpImage)
	var gpGraphics uintptr
	if ret, _, _ := gpGetGraphics.Call(gpImage, uintptr(unsafe.Pointer(&gpGraphics))); ret != 0 || gpGraphics == 0 {
		return
	}
	defer gpDeleteGraphics.Call(gpGraphics)
	gpGraphicsClear.Call(gpGraphics, 0)
	gpSetSmoothingMode.Call(gpGraphics, 4)     // SmoothingModeAntiAlias
	gpSetTextRenderingHint.Call(gpGraphics, 4) // grayscale anti-aliasing, suitable for translucent layers
	p := handyPainter{graphics: gpGraphics, dc: dc, scale: s.scale}
	// Soft shadow extends into the transparent canvas; a hard region is never used.
	// Critical: all rectangles here are logical units. Never pass the scaled
	// DIB width to a painter which itself multiplies by s.scale.
	p.pill(0x11000000, 3, 8, logicalWidth-6, handyBodyHeight)
	p.pill(0x22000000, 4, 7, logicalWidth-8, handyBodyHeight)
	p.pill(indicator.Pill.Opaque(), handyPillInset, handyBodyTop, logicalWidth-2*handyPillInset, handyBodyHeight)
	// Avoid a translucent full-size second fill: it makes a faint double edge
	// and reduces color consistency around anti-aliased corners.

	accent := indicator.Accent(s.phase, s.mode).Opaque()
	if s.phase == indicator.Recording {
		p.roundBar(accent, 12, 18, 3, 14)
		// Left pink live indicator + nine equal-width capsule bars.
		p.fill(accent, 23, indicator.IconCenterY-4, 8, 8, true)
		level := 0.0
		if s.mic != nil {
			level = float64(s.mic.level.Load()) / 1000.0
		}
		if s.mic == nil && s.owner == 0x56525348 {
			// Synthetic waveform only in explicit visual-preview mode.
			level = 0.48
		}
		s.smoothedLevel = indicator.SmoothedLevel(s.smoothedLevel, level)
		for i := 0; i < 9; i++ {
			h := indicator.WaveHeight(i, s.smoothedLevel, s.tick)
			p.roundBar(accent, indicator.WaveformX(i), indicator.IconCenterY-h/2, indicator.WaveBarWidth, h)
		}
		// Non-overlapping time and close-button lanes.
		p.text(indicator.DurationText(s.started, time.Now()), indicator.ClockX, 10, indicator.ClockWidth, 30, 14, 600, indicator.ClockText.Opaque())
	} else {
		if indicator.Terminal(s.phase) {
			// Sending Ctrl+V is not proof that the target accepted the text.
			// Keep the sent state neutral-blue; mint is reserved for visible answers.
			col := indicator.TerminalMark(s.phase).Opaque()
			p.fill(col, 22, indicator.IconCenterY-8, 16, 16, true)
			if s.phase != indicator.Error && s.phase != indicator.Fallback {
				p.checkGlyph(30, indicator.IconCenterY)
			} else {
				// Amber fallback and red error use an exclamation instead of
				// a green success check: copying is not insertion.
				p.roundBar(indicator.AttentionGlyph.Opaque(), 29, indicator.IconCenterY-5, 2, 7)
				p.fill(indicator.AttentionGlyph.Opaque(), 29, indicator.IconCenterY+4, 2, 2, true)
			}
		} else {
			// Non-blocking animated dots that form a smooth circular progress wheel.
			for i := 0; i < indicator.SpinnerDots; i++ {
				x, y, alpha := indicator.SpinnerDot(i, s.tick)
				p.fill((uint32(alpha)<<24)|(accent&0x00FFFFFF), x-indicator.SpinnerDotSize/2, y-indicator.SpinnerDotSize/2, indicator.SpinnerDotSize, indicator.SpinnerDotSize, true)
			}
		}
		p.text(indicator.Label(s.phase, s.mode), indicator.WorkingTextX, 10, logicalWidth-indicator.WorkingTextX-indicator.WorkingTextRightGap, 30, 13, 500, indicator.LabelText.Opaque())
	}
	if s.closeHovered {
		p.fill(indicator.CloseHover.Opaque(), logicalWidth-34, 11, 28, 28, true)
	}
	// Crisp anti-aliased vector X; no separate text glyph competing with the timer.
	p.closeGlyph(logicalWidth-indicator.CancelGlyphInset, indicator.IconCenterY)
	target := handyPoint{s.x, s.y}
	size := handySize{int32(width), int32(height)}
	source := handyPoint{}
	blend := handyBlend{Op: 0, Flags: 0, Alpha: indicator.StatusOpacity(s.started, s.changed, time.Now(), s.phase, s.pending), Format: 1}
	screen, _, _ := procGetDCScreenHandy.Call(0)
	if screen != 0 {
		procUpdateLayeredHandy.Call(s.window, screen, uintptr(unsafe.Pointer(&target)), uintptr(unsafe.Pointer(&size)), dc, uintptr(unsafe.Pointer(&source)), 0, uintptr(unsafe.Pointer(&blend)), handyULW_ALPHA)
		procReleaseDCScreenHandy.Call(0, screen)
	}
}

// Per-window DPI awareness makes the bar render at its monitor's native pixel
// density, instead of letting Windows stretch and blur an unscaled bitmap.
// Only the temporary status-window calls run in this context; main/editor
// windows and the Mouse4/Mouse5 input flow retain their existing DPI behavior.
func handyAwareWindowThread() func() {
	old, _, _ := procSetThreadDpiContextHandy.Call(^uintptr(3)) // PER_MONITOR_AWARE_V2 (-4)
	if old == 0 {
		return func() {}
	}
	return func() { procSetThreadDpiContextHandy.Call(old) }
}
func handyMonitorScale() float64 {
	return uxMonitorScaleForPoint(currentCursorPoint())
}
func handyScaleForMonitor(mon uintptr) float64 {
	// Deterministic DPI variants apply to explicit screenshots only.
	if os.Getenv("VOICEREWRITE_VISUAL_PREVIEW") != "" {
		switch os.Getenv("VOICEREWRITE_VISUAL_SCALE") {
		case "1.25":
			return 1.25
		case "1.5":
			return 1.5
		case "2":
			return 2
		}
	}
	if mon == 0 {
		return 1
	}
	var dpiX, dpiY uint32
	status, _, _ := procGetDpiForMonitorHandy.Call(mon, 0, uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY)))
	if status != 0 || dpiX < 96 || dpiX > 384 {
		return 1
	}
	return float64(dpiX) / 96
}

func handyScreenPosition(w, h int) (int32, int32) {
	return handyScreenPositionForMonitor(uxMonitorForPoint(currentCursorPoint()), w, h)
}
func handyScreenPositionForMonitor(mon uintptr, w, h int) (int32, int32) {
	work, ok := uxWorkAreaForMonitor(mon)
	if !ok {
		work = uxPrimaryScreen()
		return (work.Right - int32(w)) / 2, work.Bottom - int32(h) - 5
	}
	x := work.Left + (work.Right-work.Left-int32(w))/2
	y := work.Bottom - int32(h) - 5
	return desktop.Clamp(x, y, int32(w), int32(h), work)
}
func showHandyStatus(rec *audioRecorder, mode string, phase indicator.Phase, owner uint64) {
	createHandyStatus(rec, mode, phase, owner, false)
}

func showPendingHandyStatus(mode string) {
	createHandyStatus(nil, mode, indicator.Recording, 0, true)
}

// Keep the same native HWND when a held side button becomes a voice command;
// rebuilding the layered window would visibly blink at the hold threshold.
func promotePendingHandyStatus(rec *audioRecorder, mode string, owner uint64) bool {
	s := activeHandyStatus
	if s == nil || s.window == 0 || !s.pending || s.mode != mode {
		return false
	}
	s.mic = rec
	s.owner = owner
	s.pending = false
	s.changed = time.Now()
	drawHandyStatus(s)
	return true
}

func createHandyStatus(rec *audioRecorder, mode string, phase indicator.Phase, owner uint64, pending bool) {
	restore := handyAwareWindowThread()
	defer restore()
	closeHandyStatus()
	classname, _ := syscall.UTF16PtrFromString("VoiceRewrite.RecordingIndicator")
	title, _ := syscall.UTF16PtrFromString("VoiceRewrite status")
	instance, _, _ := procGetModuleHandle.Call(0)
	wc := trayWindowClass{Size: uint32(unsafe.Sizeof(trayWindowClass{})), WndProc: handyStatusWndProcCallback, Instance: instance, ClassName: classname}
	atom, _, regErr := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 && regErr != syscall.Errno(1410) {
		return
	}
	monitor := uxMonitorForPoint(currentCursorPoint())
	scale := handyScaleForMonitor(monitor)
	width, height := indicator.PhysicalSize(indicator.Width(phase), indicator.Height, scale)
	x, y := handyScreenPositionForMonitor(monitor, width, height)
	hwnd, _, _ := procCreateWindow.Call(WS_EX_TOPMOST|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE|WS_EX_LAYERED, uintptr(unsafe.Pointer(classname)), uintptr(unsafe.Pointer(title)), WS_POPUP, uintptr(x), uintptr(y), uintptr(width), uintptr(height), 0, 0, instance, 0)
	runtime.KeepAlive(classname)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		return
	}
	activeHandyStatus = &handyStatus{window: hwnd, mode: mode, mic: rec, pending: pending, phase: phase, owner: owner, started: time.Now(), changed: time.Now(), x: x, y: y, monitor: monitor, scale: scale}
	drawHandyStatus(activeHandyStatus)
	procShowWindow.Call(hwnd, SW_SHOWNOACTIVATE)
	procSetTimer.Call(hwnd, handyPulseTimer, 70, 0)
}
func updateHandyStatus(phase indicator.Phase) {
	restore := handyAwareWindowThread()
	defer restore()
	s := activeHandyStatus
	if s == nil || !indicator.Allowed(phase) {
		return
	}
	s.phase = phase
	s.changed = time.Now()
	s.scale = handyScaleForMonitor(s.monitor)
	width, height := indicator.PhysicalSize(indicator.Width(phase), indicator.Height, s.scale)
	s.x, s.y = handyScreenPositionForMonitor(s.monitor, width, height)
	// Resize the HWND with the bitmap; preserve focus and z-order.
	procSetWindowPos.Call(s.window, 0, uintptr(s.x), uintptr(s.y), uintptr(width), uintptr(height), 0x0014)
	drawHandyStatus(s)
}
func closeHandyStatus() {
	s := activeHandyStatus
	if s == nil {
		return
	}
	activeHandyStatus = nil
	procKillTimer.Call(s.window, handyPulseTimer)
	procDestroyWindow.Call(s.window)
}
