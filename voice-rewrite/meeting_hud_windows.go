//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type meetingHUD struct {
	app                                                         *appState
	window, owner                                               uintptr
	status, live, captions, responses, expression, instruction  uintptr
	currentEnglish, currentChinese, expressionSource            uintptr
	labelFont, bodyFont, englishFont, chineseFont               uintptr
	palette                                                     uiPalette
	background, surface                                         uintptr
	labels                                                      [4]uintptr
	buttons                                                     [16]uintptr
	mainControls                                                []uintptr
	profileControls                                             []uintptr
	profileBackground, profileTerms, profileMode, profileStatus uintptr
	scale                                                       float64
	fontSize                                                    int32
	paneWeights                                                 meetingPaneWeights
	paneHeights                                                 [meetingPaneCount]int32
	splitterY                                                   [5]int32
	splitterVisible                                             [5]bool
	dragDivider                                                 int
	dragY                                                       int32
	paused, captionsOnly, profileOpen, expressionExpanded       bool
	latest                                                      meetingView
	displayedReplies                                            [3]meetingLine
	displayedSource, displayedTip, copyExpression               string
	selectedReply                                               int
	followLatest                                                bool
	transcriptDrawn                                             bool
	transcriptFirstLine                                         uintptr
	historyCount, unreadHistory                                 int
	currentID                                                   uint64
	lastTranscript, lastReply, lastExpression                   time.Time
	rendered                                                    map[uintptr]string
}

var activeMeetingHUD *meetingHUD
var meetingHUDProc = syscall.NewCallback(meetingHUDWindowProc)
var meetingClassRegistered bool
var postMeetingQuit = user32.NewProc("PostQuitMessage")
var meetingEnableWindow = user32.NewProc("EnableWindow")
var meetingInvalidateRect = user32.NewProc("InvalidateRect")
var meetingSetFocus = user32.NewProc("SetFocus")
var meetingSetCapture = user32.NewProc("SetCapture")
var meetingReleaseCapture = user32.NewProc("ReleaseCapture")
var meetingScreenToClient = user32.NewProc("ScreenToClient")
var meetingGetCursorPos = user32.NewProc("GetCursorPos")
var meetingSetCursor = user32.NewProc("SetCursor")
var meetingLoadCursor = user32.NewProc("LoadCursorW")

func meetingPreferencesPath() string {
	return filepath.Join(filepath.Dir(configPath()), "meeting-ui.json")
}

func runMeetingDemo() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := showMeetingHUD(0, demoMeeting()); err != nil {
		winErr(err.Error())
		return
	}
	defer closeMeetingHUD()
	for {
		var msg winMSG
		result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			return
		}
		dispatchNativeMessage(&msg)
	}
}

func showMeetingHUD(owner uintptr, view meetingView) error {
	restoreDPI := handyAwareWindowThread()
	defer restoreDPI()
	className, _ := syscall.UTF16PtrFromString("VoiceRewrite.Meeting")
	title, _ := syscall.UTF16PtrFromString("VoiceRewrite · 会议助手")
	module, _, _ := procGetModuleHandle.Call(0)
	if !meetingClassRegistered {
		class := trayWindowClass{Size: uint32(unsafe.Sizeof(trayWindowClass{}))}
		class.Instance, class.ClassName, class.WndProc = module, className, meetingHUDProc
		if atom, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
			return fmt.Errorf("注册会议浮窗: %w", err)
		}
		meetingClassRegistered = true
	}
	anchor := currentCursorPoint()
	work, scale := uxWorkAreaForPoint(anchor), uxMonitorScaleForPoint(anchor)
	prefs, _ := loadMeetingPreferences(meetingPreferencesPath())
	x, y, w, h := prefs.geometry(work, scale)
	hwnd, _, err := procCreateWindow.Call(WS_EX_TOPMOST|WS_EX_TOOLWINDOW, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		WS_CAPTION|WS_SYSMENU|0x00040000|0x00010000|0x02000000, int32ptr(x), int32ptr(y), int32ptr(w), int32ptr(h), 0, 0, module, 0)
	runtime.KeepAlive(className)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		return fmt.Errorf("创建会议浮窗: %w", err)
	}
	err = nil
	p := &meetingHUD{window: hwnd, owner: owner, palette: resultCardPalette(), scale: scale, fontSize: prefs.FontSize, paneWeights: prefs.Panes, dragDivider: -1, captionsOnly: prefs.CaptionsOnly, followLatest: true, rendered: map[uintptr]string{}}
	activeMeetingHUD = p
	p.background, _, _ = procCreateSolidBrush.Call(p.palette.color(p.palette.background))
	p.surface, _, _ = procCreateSolidBrush.Call(p.palette.color(p.palette.surface))
	p.labelFont = meetingUIFont(p.px(14), 600)
	p.bodyFont = meetingUIFont(p.px(p.fontSize), 400)
	p.englishFont = popupFont(-p.px(p.fontSize+1), 450)
	p.chineseFont = meetingUIFont(p.px(p.fontSize+6), 600)
	applyWindowPalette(hwnd, p.palette)
	child := func(class, text string, style uintptr, id uintptr, body bool) uintptr {
		if err != nil {
			return 0
		}
		var control uintptr
		control, err = makeChild(class, text, style|WS_CHILD|WS_VISIBLE, 0, 0, 1, 1, hwnd, id)
		font := p.labelFont
		if body {
			font = p.bodyFont
		}
		setFont(control, font)
		p.mainControls = append(p.mainControls, control)
		return control
	}
	// Native tab stops include selectable read-only text and the rewrite input.
	p.status = child("STATIC", "", 0, 0, false)
	for i, title := range []string{"冻结画面", "专注字幕", "A−", "A+", "复制回复", "复制英文", "生成建议", "暂停采集", "会议资料", "1 直接回应", "2 澄清一下", "3 争取时间", "查看新建议", "改写回复", "回到最新", "放大英文"} {
		p.buttons[i] = child("BUTTON", title, 0x00010000|uxBSOwnerDraw, uintptr(7100+i), false)
	}
	p.labels[0] = child("STATIC", "当前发言 · 中文优先", 0, 0, false)
	editStyle := uintptr(WS_VSCROLL | ES_MULTILINE | ES_READONLY | ES_AUTOVSCROLL | WS_BORDER | 0x00010000)
	p.live = child("STATIC", "● 等待发言", 0, 0, false)
	currentStyle := uintptr(ES_MULTILINE | ES_READONLY | ES_AUTOVSCROLL | WS_VSCROLL)
	p.currentEnglish = child("EDIT", "", currentStyle, 0, false)
	p.currentChinese = child("EDIT", "", currentStyle, 0, false)
	for _, control := range []uintptr{p.currentEnglish, p.currentChinese} {
		margin := p.px(10)
		procSendMessage.Call(control, uxEMSetMargins, 3, uintptr(uint32(margin)|uint32(margin)<<16))
	}
	setFont(p.currentEnglish, p.englishFont)
	setFont(p.currentChinese, p.chineseFont)
	p.labels[3] = child("STATIC", "之前的对话", 0, 0, false)
	p.captions = child("EDIT", "", editStyle, 0, true)
	procSendMessage.Call(p.captions, 0x00C5, 1<<20, 0) // EM_LIMITTEXT: retain in-session history
	p.labels[1] = child("STATIC", "回复建议", 0, 0, false)
	p.responses = child("EDIT", "", editStyle, 0, true)
	p.instruction = child("EDIT", "", WS_BORDER|0x00010000|0x0080, 0, true)
	procSendMessage.Call(p.instruction, 0x00C5, 600, 0) // EM_LIMITTEXT
	cue, _ := syscall.UTF16PtrFromString("改写要求，例如：更简短、先说明风险")
	procSendMessage.Call(p.instruction, 0x1501, 1, uintptr(unsafe.Pointer(cue))) // EM_SETCUEBANNER
	runtime.KeepAlive(cue)
	p.labels[2] = child("STATIC", "我的英文表达", 0, 0, false)
	p.expression = child("EDIT", "", editStyle, 0, true)
	setFont(p.expression, p.englishFont)
	p.expressionSource = child("EDIT", "", ES_MULTILINE|ES_READONLY|ES_AUTOVSCROLL|WS_VSCROLL, 0, false)
	if err != nil {
		closeMeetingHUD()
		return err
	}
	p.layout()
	updateMeetingHUD(view)
	procShowWindow.Call(hwnd, SW_SHOWNOACTIVATE)
	return nil
}

// A Chinese-capable sans-serif avoids the SimSun fallback visible on older
// Windows installations when Segoe UI Variable is used for mixed CJK text.
func meetingUIFont(height, weight int32) uintptr {
	name, _ := syscall.UTF16PtrFromString("Microsoft YaHei UI")
	font, _, _ := procCreateFont.Call(uintptr(-height), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(name)))
	runtime.KeepAlive(name)
	return font
}

func (p *meetingHUD) px(v int32) int32 { return int32(float64(v)*p.scale + .5) }
func (p *meetingHUD) move(control uintptr, x, y, w, h int32) {
	procMoveWindow.Call(control, int32ptr(x), int32ptr(y), int32ptr(max(1, w)), int32ptr(max(1, h)), 1)
}
func (p *meetingHUD) layout() {
	var bounds rect32
	procGetClientRect.Call(p.window, uintptr(unsafe.Pointer(&bounds)))
	margin, gap, label, button := p.px(16), p.px(8), p.px(24), p.px(32)
	width := max(p.px(200), bounds.Right-2*margin)
	for _, control := range p.mainControls {
		procShowWindow.Call(control, meetingShow(!p.profileOpen))
	}
	p.splitterVisible = [5]bool{}
	for _, control := range p.profileControls {
		procShowWindow.Call(control, meetingShow(p.profileOpen))
	}
	if p.profileOpen {
		p.layoutProfile(width, bounds.Bottom)
		return
	}

	// Status and secondary controls occupy two compact rows. Reading always
	// starts at the same Y coordinate, regardless of reply suggestions.
	y := p.px(8)
	p.move(p.status, margin, y, width, p.px(40))
	y += p.px(40) + p.px(6)
	x := margin
	for _, item := range []struct {
		index int
		width int32
	}{{7, 96}, {1, 92}, {0, 90}, {2, 36}, {3, 36}, {8, 92}} {
		p.move(p.buttons[item.index], x, y, p.px(item.width), button)
		x += p.px(item.width) + gap
	}
	y += button + gap
	p.move(p.labels[0], margin, y, width-p.px(264), label)
	p.move(p.live, margin+width-p.px(264), y, p.px(264), label)
	y += label + p.px(3)

	// Six content surfaces share the remaining height. Divider gaps are
	// mouse-draggable and the proportions survive window resizing.
	footer := bounds.Bottom - margin
	overhead := int32(5*8 + 24 + 8 + 120 + 24)
	if p.captionsOnly {
		overhead = 2*8 + 24 + 8
	} else if p.expressionExpanded {
		overhead = 4*8 + 24 + 8 + 24
	}
	space := int32(float64(footer-y)/p.scale) - overhead
	p.paneHeights = meetingPaneHeights(space, p.paneWeights, p.captionsOnly, p.expressionExpanded)
	divider := func(index int) {
		p.splitterY[index] = y + p.px(4)
		p.splitterVisible[index] = true
		y += p.px(8)
	}
	p.move(p.currentChinese, margin, y, width, p.px(p.paneHeights[meetingPaneChinese]))
	y += p.px(p.paneHeights[meetingPaneChinese])
	divider(0)
	p.move(p.currentEnglish, margin, y, width, p.px(p.paneHeights[meetingPaneEnglish]))
	y += p.px(p.paneHeights[meetingPaneEnglish])
	divider(1)
	p.move(p.labels[3], margin, y, width-p.px(114), label)
	p.move(p.buttons[14], margin+width-p.px(110), y-p.px(3), p.px(110), button)
	y += label + gap
	p.move(p.captions, margin, y, width, p.px(p.paneHeights[meetingPaneHistory]))
	y += p.px(p.paneHeights[meetingPaneHistory])

	for _, control := range []uintptr{p.labels[1], p.responses, p.instruction, p.buttons[4], p.buttons[6], p.buttons[9], p.buttons[10], p.buttons[11], p.buttons[12], p.buttons[13]} {
		procShowWindow.Call(control, meetingShow(!p.captionsOnly && !p.expressionExpanded))
	}
	for _, control := range []uintptr{p.labels[2], p.expression, p.expressionSource, p.buttons[5], p.buttons[15]} {
		procShowWindow.Call(control, meetingShow(!p.captionsOnly))
	}
	if p.captionsOnly {
		meetingInvalidateRect.Call(p.window, 0, 1)
		return
	}

	divider(2)
	if !p.expressionExpanded {
		p.move(p.labels[1], margin, y, width-p.px(316), button)
		p.move(p.buttons[6], margin+width-p.px(310), y, p.px(92), button)
		p.move(p.buttons[12], margin+width-p.px(210), y, p.px(108), button)
		p.move(p.buttons[4], margin+width-p.px(94), y, p.px(94), button)
		y += button + gap
		x = margin
		for _, item := range []struct {
			index int
			width int32
		}{{9, 112}, {10, 112}, {11, 112}} {
			p.move(p.buttons[item.index], x, y, p.px(item.width), button)
			x += p.px(item.width) + gap
		}
		y += button + gap
		p.move(p.responses, margin, y, width, p.px(p.paneHeights[meetingPaneReply]))
		y += p.px(p.paneHeights[meetingPaneReply]) + gap
		p.move(p.instruction, margin, y, width-p.px(112)-gap, button)
		p.move(p.buttons[13], margin+width-p.px(112), y, p.px(112), button)
		y += button
		divider(3)
	}

	p.move(p.labels[2], margin, y, width-p.px(240), label)
	p.move(p.buttons[15], margin+width-p.px(226), y-p.px(4), p.px(110), button)
	p.move(p.buttons[5], margin+width-p.px(112), y-p.px(4), p.px(112), button)
	y += label
	p.move(p.expression, margin, y, width, p.px(p.paneHeights[meetingPaneExpression]))
	y += p.px(p.paneHeights[meetingPaneExpression])
	divider(4)
	p.move(p.expressionSource, margin, y, width, p.px(p.paneHeights[meetingPaneSource]))
	meetingInvalidateRect.Call(p.window, 0, 1)
}

// Divider hit regions are the unobstructed eight-pixel bands between
// independent EDIT controls. Capture keeps the drag active outside the band.
func (p *meetingHUD) splitterAt(y int32) int {
	if p.profileOpen {
		return -1
	}
	for index, visible := range p.splitterVisible {
		if visible && y >= p.splitterY[index]-p.px(4) && y < p.splitterY[index]+p.px(4) {
			return index
		}
	}
	return -1
}

func (p *meetingHUD) dragSplitterTo(y int32) {
	if p.dragDivider < 0 {
		return
	}
	delta := int32(float64(y-p.dragY) / p.scale)
	if delta == 0 {
		return
	}
	p.dragY = y
	weights := meetingDragPaneWeights(p.paneWeights, p.paneHeights, p.captionsOnly, p.expressionExpanded, p.dragDivider, delta)
	if weights != p.paneWeights {
		p.paneWeights = weights
		p.layout()
	}
}

func meetingShow(show bool) uintptr {
	if show {
		return SW_SHOWNOACTIVATE
	}
	return SW_HIDE
}
func meetingBool(value bool) uintptr {
	if value {
		return 1
	}
	return 0
}

func (p *meetingHUD) resizeText(delta int32) {
	size := max(12, min(28, p.fontSize+delta))
	if size == p.fontSize {
		return
	}
	body := meetingUIFont(p.px(size), 400)
	english := popupFont(-p.px(size+1), 450)
	chinese := meetingUIFont(p.px(size+6), 600)
	if body == 0 || english == 0 || chinese == 0 {
		for _, font := range []uintptr{body, english, chinese} {
			if font != 0 {
				procDeleteObject.Call(font)
			}
		}
		return
	}
	old := []uintptr{p.bodyFont, p.englishFont, p.chineseFont}
	p.bodyFont, p.englishFont, p.chineseFont, p.fontSize = body, english, chinese, size
	for _, control := range []uintptr{p.captions, p.responses, p.instruction, p.profileBackground, p.profileTerms} {
		if control != 0 {
			setFont(control, body)
		}
	}
	setFont(p.currentEnglish, english)
	setFont(p.currentChinese, chinese)
	setFont(p.expression, english)
	for _, font := range old {
		procDeleteObject.Call(font)
	}
	p.layout()
	p.savePreferences()
}

// The user decides whether live captions follow the latest turn. A scroll
// away from the bottom suspends following; a return to the bottom resumes it.
func (p *meetingHUD) transcriptAtBottom(control uintptr) bool {
	info := uxScrollInfo{Size: uint32(unsafe.Sizeof(uxScrollInfo{})), Mask: uxSIFRange | uxSIFPage | uxSIFPos}
	ok, _, _ := procGetScrollInfoUX.Call(control, uxSBVert, uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return false
	}
	remaining := info.Max - info.Pos - max(0, int32(info.Page)-1)
	return remaining <= 2
}

func (p *meetingHUD) scrollToLatest() {
	p.followLatest = true
	p.unreadHistory = 0
	// EM_LINESCROLL moves the viewport, not the selection/caret.
	procSendMessage.Call(p.captions, 0x00B6, 0, uintptr(1<<20))
	p.transcriptFirstLine, _, _ = procSendMessage.Call(p.captions, 0x00CE, 0, 0)
	updateMeetingHUD(p.latest)
}

func (p *meetingHUD) setText(control uintptr, value string, preserve bool) {
	previous := p.rendered[control]
	if value == previous {
		return
	}
	if !preserve {
		uxSetText(control, value)
		p.rendered[control] = value
		return
	}

	// WM_SETTEXT resets native EDIT scroll and selection on every update.
	// Patch only the changed suffix. This leaves all committed lines intact
	// and avoids the top-of-document jump during active transcription.
	first, _, _ := procSendMessage.Call(control, 0x00CE, 0, 0) // EM_GETFIRSTVISIBLELINE
	if control == p.captions && p.transcriptDrawn {
		p.followLatest = meetingFollowLatest(p.followLatest, p.transcriptFirstLine != first, p.transcriptAtBottom(control))
	}
	follow := control == p.captions && p.followLatest
	var start, end uint32
	procSendMessage.Call(control, 0x00B0, uintptr(unsafe.Pointer(&start)), uintptr(unsafe.Pointer(&end))) // EM_GETSEL
	change := meetingTextChange(previous, value)

	procSendMessage.Call(control, 0x000B, 0, 0)                                       // WM_SETREDRAW off
	procSendMessage.Call(control, 0x00CF, 0, 0)                                       // EM_SETREADONLY off for programmatic edit
	procSendMessage.Call(control, 0x00B1, uintptr(change.start), uintptr(change.end)) // EM_SETSEL
	insert := uxPtr(change.insert)
	procSendMessage.Call(control, 0x00C2, 0, uintptr(unsafe.Pointer(insert))) // EM_REPLACESEL
	runtime.KeepAlive(insert)
	procSendMessage.Call(control, 0x00CF, 1, 0) // EM_SETREADONLY on
	procSendMessage.Call(control, 0x00B1,
		uintptr(meetingMapSelection(start, change)),
		uintptr(meetingMapSelection(end, change)))
	if follow {
		procSendMessage.Call(control, 0x00B6, 0, uintptr(1<<20)) // EM_LINESCROLL down
	} else {
		now, _, _ := procSendMessage.Call(control, 0x00CE, 0, 0)
		procSendMessage.Call(control, 0x00B6, 0, uintptr(int64(first)-int64(now)))
	}
	procSendMessage.Call(control, 0x000B, 1, 0) // WM_SETREDRAW on
	p.rendered[control] = value
	if control == p.captions {
		p.transcriptDrawn = true
		p.transcriptFirstLine, _, _ = procSendMessage.Call(control, 0x00CE, 0, 0)
	}
	meetingInvalidateRect.Call(control, 0, 1)
}

func updateMeetingHUD(view meetingView) {
	p := activeMeetingHUD
	if p == nil {
		return
	}
	p.latest = view
	source := strings.Join(strings.Fields(view.sourceName), " ")
	if source == "" {
		source = "等待音频源"
	}
	if runes := []rune(source); len(runes) > 34 {
		source = string(runes[:33]) + "…"
	}
	state := view.status
	if view.capturePaused {
		state = "采集已暂停 · 不再接收音频"
	} else if p.paused {
		state = "画面已冻结 · 后台继续处理"
	} else if state == "" {
		state = "正在聆听"
	}
	detail := source
	if view.metrics.Stage != "" {
		detail += "  ·  " + view.metrics.Stage
	}
	if view.metrics.Dropped > 0 {
		detail += fmt.Sprintf("  ·  跳过 %d 个过期片段", view.metrics.Dropped)
	}
	p.setText(p.status, state+"\r\n"+detail, false)
	p.setText(p.buttons[0], map[bool]string{false: "冻结画面", true: "恢复画面"}[p.paused], false)
	p.setText(p.buttons[7], map[bool]string{false: "暂停采集", true: "恢复采集"}[view.capturePaused], false)
	p.setText(p.buttons[1], map[bool]string{false: "专注字幕", true: "完整视图"}[p.captionsOnly], false)
	p.setText(p.buttons[15], map[bool]string{false: "放大英文", true: "收起英文"}[p.expressionExpanded], false)
	for _, i := range []int{6, 7, 8} {
		meetingEnableWindow.Call(p.buttons[i], meetingBool(p.owner != 0))
	}
	meetingEnableWindow.Call(p.buttons[12], meetingBool(p.owner != 0 && view.replyPending && !p.paused))
	p.setText(p.buttons[12], map[bool]string{false: "无新建议", true: "查看新建议"}[view.replyPending], false)
	if p.paused {
		return
	}
	panels := buildMeetingReadingPanels(view.captions)
	newTurn := panels.currentID != p.currentID || panels.historyCount > p.historyCount
	p.currentID = panels.currentID
	p.setText(p.live, "● "+panels.hint, false)
	if newTurn || !panels.live || time.Since(p.lastTranscript) >= 280*time.Millisecond {
		p.setText(p.currentEnglish, panels.english, true)
		p.setText(p.currentChinese, panels.chinese, true)
		if newTurn {
			// Reading a fresh utterance starts at its first line, not the
			// scroll position of the previously long utterance.
			procSendMessage.Call(p.currentEnglish, 0x0115, 6, 0) // WM_VSCROLL / SB_TOP
			procSendMessage.Call(p.currentChinese, 0x0115, 6, 0)
		}
		p.lastTranscript = time.Now()
	}
	// Only completed older turns are added to the history control. If the
	// reader scrolled away, count unseen turns instead of moving the viewport.
	if panels.history != p.rendered[p.captions] {
		p.setText(p.captions, panels.history, true)
		p.unreadHistory = meetingUnreadHistory(p.unreadHistory, p.historyCount, panels.historyCount, p.followLatest)
		p.historyCount = panels.historyCount
	}
	if p.followLatest {
		p.unreadHistory = 0
	}
	historyLabel := fmt.Sprintf("之前的对话 · %d 段", panels.historyCount)
	if p.unreadHistory > 0 {
		historyLabel += fmt.Sprintf(" · %d 条新内容", p.unreadHistory)
	}
	p.setText(p.labels[3], historyLabel, false)
	p.displayedReplies, p.displayedSource, p.displayedTip = view.replies, view.replySource, view.tip
	p.renderReply()
	newEnglish := view.expression.complete && view.expression.english != "" && view.expression.english != p.copyExpression
	captionsOnly, expanded := meetingExpressionFocus(view.expression.complete, newEnglish, p.captionsOnly, p.expressionExpanded)
	if captionsOnly != p.captionsOnly || expanded != p.expressionExpanded {
		p.captionsOnly, p.expressionExpanded = captionsOnly, expanded
		p.layout()
	}
	// Show the last successful utterance while recognition or generation is
	// underway. Transient status belongs below the result, not in its place.
	expression := view.expression.english
	if view.expression.complete {
		p.copyExpression = view.expression.english
	} else {
		p.copyExpression = ""
	}
	if expression == "" {
		expression = "听写键：直译自己的发言；改写键：提问、回答、补充观点。"
	}
	p.setText(p.labels[2], meetingExpressionHeading(view.expressionStatus, view.expression.complete), false)
	if view.expression.complete || time.Since(p.lastExpression) >= 280*time.Millisecond {
		p.setText(p.expression, expression, true)
		if newEnglish {
			procSendMessage.Call(p.expression, 0x0115, 6, 0) // WM_VSCROLL / SB_TOP
		}
		p.lastExpression = time.Now()
	}
	original := meetingExpressionDetail(view.expressionStatus, view.expressionSource)
	p.setText(p.expressionSource, original, true)
	meetingEnableWindow.Call(p.buttons[5], meetingBool(p.copyExpression != ""))
}

func (p *meetingHUD) renderReply() {
	reply := p.displayedReplies[p.selectedReply]
	names := [3]string{"直接回应", "澄清一下", "争取时间"}
	for i := range names {
		title := fmt.Sprintf("%d %s", i+1, names[i])
		if i == p.selectedReply {
			title = "● " + title
		}
		p.setText(p.buttons[9+i], title, false)
	}
	text := reply.english + "\r\n" + reply.chinese
	if reply.english != "" && !reply.complete {
		text += "\r\n正在生成…（完成后可复制或改写）"
	}
	if reply.english == "" {
		text = "听到提问后生成建议；新建议可点击“查看新建议”后阅读。"
	}
	if p.displayedTip != "" {
		text += "\r\n提示 · " + p.displayedTip
	}
	if reply.complete || time.Since(p.lastReply) >= 280*time.Millisecond {
		p.setText(p.responses, text, true)
		p.lastReply = time.Now()
	}
	meetingEnableWindow.Call(p.buttons[4], meetingBool(reply.complete && reply.english != ""))
	meetingEnableWindow.Call(p.buttons[13], meetingBool(p.owner != 0 && reply.complete && reply.english != "" && !p.paused))
}

func (p *meetingHUD) savePreferences() {
	var bounds rect32
	if ok, _, _ := popupGetWindowRect.Call(p.window, uintptr(unsafe.Pointer(&bounds))); ok == 0 {
		return
	}
	// Do not persist minimized or maximized geometry as a normal window size.
	minimized, _, _ := user32.NewProc("IsIconic").Call(p.window)
	maximized, _, _ := user32.NewProc("IsZoomed").Call(p.window)
	prefs, _ := loadMeetingPreferences(meetingPreferencesPath())
	prefs.FontSize, prefs.CaptionsOnly, prefs.Panes = p.fontSize, p.captionsOnly, p.paneWeights
	if minimized == 0 && maximized == 0 {
		prefs.X, prefs.Y, prefs.Width, prefs.Height = bounds.Left, bounds.Top, bounds.Right-bounds.Left, bounds.Bottom-bounds.Top
	}
	if err := saveMeetingPreferences(meetingPreferencesPath(), prefs); err != nil && p.app != nil {
		p.app.logger.Printf("meeting UI preferences: %v", err)
	}
}

func closeMeetingHUD() {
	p := activeMeetingHUD
	if p == nil {
		return
	}
	p.savePreferences()
	activeMeetingHUD = nil
	procDestroyWindow.Call(p.window)
	for _, handle := range []uintptr{p.labelFont, p.bodyFont, p.englishFont, p.chineseFont, p.background, p.surface} {
		if handle != 0 {
			procDeleteObject.Call(handle)
		}
	}
}

func meetingHUDWindowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if p := activeMeetingHUD; p != nil && p.window == hwnd {
		switch msg {
		case 0x0005:
			p.layout()
			return 0
		case 0x0232:
			p.savePreferences()
			return 0 // WM_EXITSIZEMOVE
		case 0x0024:
			info := (*uxMinMaxInfo)(unsafe.Pointer(lParam))
			var bounds rect32
			popupGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
			work := uxWorkAreaForPoint(trayPoint{X: bounds.Left, Y: bounds.Top})
			w, h := desktopFitSize(p.px(600), p.px(720), work, 0)
			info.MinSize = trayPoint{X: w, Y: h}
			return 0

		case uxWMDrawItem:
			if lParam != 0 {
				item := (*drawItemStruct)(unsafe.Pointer(lParam))
				if item.CtlID >= 7100 && item.CtlID <= 7115 {
					uiDrawActionButton(item, p.palette, p.labelFont, uxWinText(p.buttons[item.CtlID-7100]), item.CtlID == 7106 || item.CtlID == 7112 && p.latest.replyPending || item.CtlID == 7115 && p.expressionExpanded)
					return 1
				}
				if item.CtlID == 7205 || item.CtlID == 7206 {
					uiDrawActionButton(item, p.palette, p.labelFont, uxWinText(item.HwndItem), item.CtlID == 7205)
					return 1
				}
			}
		case 0x0020: // WM_SETCURSOR
			if wParam == hwnd {
				var mouse trayPoint
				if ok, _, _ := meetingGetCursorPos.Call(uintptr(unsafe.Pointer(&mouse))); ok != 0 {
					meetingScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&mouse)))
					if p.dragDivider >= 0 || p.splitterAt(mouse.Y) >= 0 {
						cursor, _, _ := meetingLoadCursor.Call(0, 32645) // IDC_SIZENS
						meetingSetCursor.Call(cursor)
						return 1
					}
				}
			}
		case 0x0201: // WM_LBUTTONDOWN
			y := int32(int16(uint16(lParam >> 16)))
			if divider := p.splitterAt(y); divider >= 0 {
				p.dragDivider = divider
				p.dragY = y
				meetingSetCapture.Call(hwnd)
				return 0
			}
		case 0x0200: // WM_MOUSEMOVE
			if p.dragDivider >= 0 {
				p.dragSplitterTo(int32(int16(uint16(lParam >> 16))))
				return 0
			}
		case 0x0202: // WM_LBUTTONUP
			if p.dragDivider >= 0 {
				p.dragSplitterTo(int32(int16(uint16(lParam >> 16))))
				p.dragDivider = -1
				meetingReleaseCapture.Call()
				p.savePreferences()
				return 0
			}
		case 0x0215: // WM_CAPTURECHANGED
			p.dragDivider = -1
		case WM_COMMAND:
			if wParam>>16 != 0 {
				break
			}
			switch wParam & 0xffff {
			case 7100:
				p.paused = !p.paused
				updateMeetingHUD(p.latest)
			case 7101:
				p.captionsOnly = !p.captionsOnly
				p.layout()
				p.savePreferences()
				updateMeetingHUD(p.latest)
			case 7102:
				p.resizeText(-2)
			case 7103:
				p.resizeText(2)
			case 7104, 7105:
				value := p.displayedReplies[p.selectedReply].english
				if !p.displayedReplies[p.selectedReply].complete {
					value = ""
				}
				if wParam&0xffff == 7105 {
					value = p.copyExpression
				}
				if value != "" {
					if err := setClipboardText(hwnd, value); err != nil {
						winErr("复制失败: " + err.Error())
					}
				}
			case 7106:
				if p.app != nil && p.app.meeting != nil {
					p.app.meeting.requestReplies()
				}
			case 7107:
				if p.app != nil && p.app.meeting != nil {
					p.app.meeting.setCapturePaused(!p.latest.capturePaused)
					p.app.refreshMeeting()
				}
			case 7108:
				p.openProfile()
			case 7109, 7110, 7111:
				p.selectedReply = int(wParam&0xffff) - 7109
				p.lastReply = time.Time{}
				p.renderReply()
			case 7114:
				p.scrollToLatest()
			case 7115:
				p.expressionExpanded = !p.expressionExpanded
				p.layout()
				updateMeetingHUD(p.latest)
			case 7112:
				if p.app != nil && p.app.meeting != nil && !p.paused {
					p.app.meeting.acceptPendingReplies()
					p.app.refreshMeeting()
				}
			case 7113:
				instruction := strings.TrimSpace(uxWinText(p.instruction))
				if instruction != "" && p.displayedReplies[p.selectedReply].complete && p.app != nil && p.app.meeting != nil && !p.paused {
					p.app.meeting.refineReply(p.selectedReply, instruction)
				}
			case 7205:
				p.saveProfile()
			case 7206:
				p.profileOpen = false
				p.layout()
			}
			return 0
		case WM_TIMER:
			if wParam == 1 && p.app != nil {
				p.app.refreshMeeting()
			}
			return 0
		case WM_CLOSE:
			if p.owner == 0 {
				closeMeetingHUD()
				postMeetingQuit.Call(0)
			} else {
				procPostMessage.Call(p.owner, WM_MEETING_STOP, 0, 0)
			}
			return 0
		case WM_CTLCOLOREDIT, WM_CTLCOLORSTATIC:
			if lParam == p.live {
				procSetTextColor.Call(wParam, p.palette.color(p.palette.primary))
			} else {
				procSetTextColor.Call(wParam, p.palette.color(p.palette.text))
			}
			if lParam == p.captions || lParam == p.live || lParam == p.currentEnglish || lParam == p.currentChinese || lParam == p.responses || lParam == p.expression || lParam == p.expressionSource || lParam == p.instruction || lParam == p.profileBackground || lParam == p.profileTerms {
				procSetBkColor.Call(wParam, p.palette.color(p.palette.surface))
				return p.surface
			}
			procSetBkColor.Call(wParam, p.palette.color(p.palette.background))
			return p.background
		case WM_ERASEBKGND:
			var bounds rect32
			procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
			procFillRect.Call(wParam, uintptr(unsafe.Pointer(&bounds)), p.background)
			if !p.profileOpen {
				for index, shown := range p.splitterVisible {
					if !shown {
						continue
					}
					for offset := int32(-2); offset <= 2; offset += 4 {
						rail := rect32{Left: bounds.Right/2 - p.px(22), Top: p.splitterY[index] + offset, Right: bounds.Right/2 + p.px(22), Bottom: p.splitterY[index] + offset + p.px(1)}
						procFillRect.Call(wParam, uintptr(unsafe.Pointer(&rail)), p.surface)
					}
				}
			}
			return 1
		}
	}
	result, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return result
}

func demoMeeting() meetingView {
	return meetingView{
		status:     "演示数据 · 试用界面无需音频或模型",
		sourceName: "Zoom.exe · 演示会议", asrMode: "multipart", level: 36,
		metrics: meetingMetrics{Stage: "聆听", ASRFirstMS: 340, ASRDoneMS: 720, TranslationFirstMS: 120, TranslationDoneMS: 310},
		captions: []meetingCaption{
			{final: true, source: "The impact seems to be limited to peak hours.", chinese: "影响似乎仅限于高峰时段。"},
			{final: true, source: "We're seeing high tail latency on vector search. How would you start investigating?", chinese: "向量检索的尾延迟偏高。你会从哪里开始排查？"},
		},
		replies: [3]meetingLine{
			{kind: "ONE", complete: true, english: "I'd start by separating time spent waiting in the queue from time spent running the search.", chinese: "我会先把排队耗时和实际检索耗时分开看。"},
			{kind: "TWO", complete: true, english: "Is the spike limited to certain queries, or does it affect the whole cluster?", chinese: "是部分查询出现尖峰，还是整个集群都受影响？"},
			{kind: "THREE", complete: true, english: "Let me take a moment to think through where we'd get the clearest signal.", chinese: "我先理一下从哪里入手能最快弄清情况。"},
		},
		replySource: "We're seeing high tail latency on vector search. How would you start investigating?",
		tip:         "先对照高峰期的 p99 排队时间与分片耗时，再核对是否集中在特定索引或查询类型。",
		expression:  meetingLine{kind: "SAY", complete: true, english: "We can do that, but by Friday I can only share an initial version.", chinese: "可以做，但周五前我只能给初版。"},
	}
}
