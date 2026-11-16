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

	"voicerewrite/internal/core/desktop"
	meetingcore "voicerewrite/internal/core/meeting"
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
	paneWeights                                                 meetingcore.PaneWeights
	paneHeights                                                 [meetingcore.PaneCount]int32
	splitterY                                                   [5]int32
	splitterVisible                                             [5]bool
	dragDivider                                                 int
	dragY                                                       int32
	paused, captionsOnly, profileOpen, expressionExpanded       bool
	latest                                                      meetingView
	displayedReplies                                            [3]meetingcore.Line
	displayedTip, copyExpression                                string
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
	prefs, _ := meetingcore.LoadPreferences(meetingPreferencesPath())
	x, y, w, h := meetingcore.Place(prefs, work, scale)
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
	idle := meetingcore.ButtonView{SelectedReply: -1}
	for i := 0; i <= meetingcore.ButtonExpression-meetingcore.ButtonPause; i++ {
		id := meetingcore.ButtonPause + i
		p.buttons[i] = child("BUTTON", meetingcore.ButtonFace(id, idle), 0x00010000|uxBSOwnerDraw, uintptr(id), false)
	}
	p.labels[0] = child("STATIC", meetingcore.SpeakerLabel, 0, 0, false)
	editStyle := uintptr(WS_VSCROLL | ES_MULTILINE | ES_READONLY | ES_AUTOVSCROLL | WS_BORDER | 0x00010000)
	p.live = child("STATIC", meetingcore.LiveCaption(meetingcore.WaitingHint), 0, 0, false)
	currentStyle := uintptr(ES_MULTILINE | ES_READONLY | ES_AUTOVSCROLL | WS_VSCROLL)
	p.currentEnglish = child("EDIT", "", currentStyle, 0, false)
	p.currentChinese = child("EDIT", "", currentStyle, 0, false)
	for _, control := range []uintptr{p.currentEnglish, p.currentChinese} {
		margin := p.px(10)
		procSendMessage.Call(control, uxEMSetMargins, 3, uintptr(uint32(margin)|uint32(margin)<<16))
	}
	setFont(p.currentEnglish, p.englishFont)
	setFont(p.currentChinese, p.chineseFont)
	p.labels[3] = child("STATIC", meetingcore.HistoryTitle, 0, 0, false)
	p.captions = child("EDIT", "", editStyle, 0, true)
	procSendMessage.Call(p.captions, 0x00C5, 1<<20, 0) // EM_LIMITTEXT: retain in-session history
	p.labels[1] = child("STATIC", meetingcore.ReplyLabel, 0, 0, false)
	p.responses = child("EDIT", "", editStyle, 0, true)
	p.instruction = child("EDIT", "", WS_BORDER|0x00010000|0x0080, 0, true)
	procSendMessage.Call(p.instruction, 0x00C5, 600, 0) // EM_LIMITTEXT
	cue, _ := syscall.UTF16PtrFromString(meetingcore.RefineCue)
	procSendMessage.Call(p.instruction, 0x1501, 1, uintptr(unsafe.Pointer(cue))) // EM_SETCUEBANNER
	runtime.KeepAlive(cue)
	p.labels[2] = child("STATIC", meetingcore.ExpressionHeading("", false), 0, 0, false)
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
	width := max(p.px(200), bounds.Right-2*p.px(16))
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
	lay := meetingcore.LayoutReading(bounds.Right, bounds.Bottom, p.scale, p.paneWeights, p.captionsOnly, p.expressionExpanded)
	p.paneHeights = lay.PaneHeights
	p.splitterY, p.splitterVisible = lay.SplitterY, lay.SplitterOn
	p.move(p.status, lay.Status.X, lay.Status.Y, lay.Status.W, lay.Status.H)
	indexes, _ := meetingcore.TopToolbar()
	for i, index := range indexes {
		box := lay.Toolbar[i]
		p.move(p.buttons[index], box.X, box.Y, box.W, box.H)
	}
	p.move(p.labels[0], lay.Speaker.X, lay.Speaker.Y, lay.Speaker.W, lay.Speaker.H)
	p.move(p.live, lay.Live.X, lay.Live.Y, lay.Live.W, lay.Live.H)
	p.move(p.currentChinese, lay.Chinese.X, lay.Chinese.Y, lay.Chinese.W, lay.Chinese.H)
	p.move(p.currentEnglish, lay.English.X, lay.English.Y, lay.English.W, lay.English.H)
	p.move(p.labels[3], lay.HistoryLabel.X, lay.HistoryLabel.Y, lay.HistoryLabel.W, lay.HistoryLabel.H)
	p.move(p.buttons[14], lay.HistoryButton.X, lay.HistoryButton.Y, lay.HistoryButton.W, lay.HistoryButton.H)
	p.move(p.captions, lay.History.X, lay.History.Y, lay.History.W, lay.History.H)
	shown := meetingcore.VisiblePanes(p.captionsOnly, p.expressionExpanded)
	for _, control := range []uintptr{p.labels[1], p.responses, p.instruction, p.buttons[4], p.buttons[6], p.buttons[9], p.buttons[10], p.buttons[11], p.buttons[12], p.buttons[13]} {
		procShowWindow.Call(control, meetingShow(shown[meetingcore.PaneReply]))
	}
	for _, control := range []uintptr{p.labels[2], p.expression, p.expressionSource, p.buttons[5], p.buttons[15]} {
		procShowWindow.Call(control, meetingShow(shown[meetingcore.PaneExpression]))
	}
	if lay.Reply {
		p.move(p.labels[1], lay.ReplyLabel.X, lay.ReplyLabel.Y, lay.ReplyLabel.W, lay.ReplyLabel.H)
		p.move(p.buttons[6], lay.Request.X, lay.Request.Y, lay.Request.W, lay.Request.H)
		p.move(p.buttons[12], lay.Accept.X, lay.Accept.Y, lay.Accept.W, lay.Accept.H)
		p.move(p.buttons[4], lay.CopyReply.X, lay.CopyReply.Y, lay.CopyReply.W, lay.CopyReply.H)
		indexes, _ = meetingcore.ReplyToolbar()
		for i, index := range indexes {
			box := lay.ReplyButtons[i]
			p.move(p.buttons[index], box.X, box.Y, box.W, box.H)
		}
		p.move(p.responses, lay.Responses.X, lay.Responses.Y, lay.Responses.W, lay.Responses.H)
		p.move(p.instruction, lay.Instruction.X, lay.Instruction.Y, lay.Instruction.W, lay.Instruction.H)
		p.move(p.buttons[13], lay.Refine.X, lay.Refine.Y, lay.Refine.W, lay.Refine.H)
	}
	if lay.Expression {
		p.move(p.labels[2], lay.ExpressionLabel.X, lay.ExpressionLabel.Y, lay.ExpressionLabel.W, lay.ExpressionLabel.H)
		p.move(p.buttons[15], lay.Expand.X, lay.Expand.Y, lay.Expand.W, lay.Expand.H)
		p.move(p.buttons[5], lay.CopyExpression.X, lay.CopyExpression.Y, lay.CopyExpression.W, lay.CopyExpression.H)
		p.move(p.expression, lay.ExpressionBody.X, lay.ExpressionBody.Y, lay.ExpressionBody.W, lay.ExpressionBody.H)
		p.move(p.expressionSource, lay.Source.X, lay.Source.Y, lay.Source.W, lay.Source.H)
	}
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
	weights := meetingcore.DragPaneWeights(p.paneWeights, p.paneHeights, p.captionsOnly, p.expressionExpanded, p.dragDivider, delta)
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
	size := meetingcore.NextFontSize(p.fontSize, delta)
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
		p.followLatest = meetingcore.FollowLatest(p.followLatest, p.transcriptFirstLine != first, p.transcriptAtBottom(control))
	}
	follow := control == p.captions && p.followLatest
	var start, end uint32
	procSendMessage.Call(control, 0x00B0, uintptr(unsafe.Pointer(&start)), uintptr(unsafe.Pointer(&end))) // EM_GETSEL
	change := meetingcore.TextChange(previous, value)

	procSendMessage.Call(control, 0x000B, 0, 0)                                       // WM_SETREDRAW off
	procSendMessage.Call(control, 0x00CF, 0, 0)                                       // EM_SETREADONLY off for programmatic edit
	procSendMessage.Call(control, 0x00B1, uintptr(change.Start), uintptr(change.End)) // EM_SETSEL
	insert := uxPtr(change.Insert)
	procSendMessage.Call(control, 0x00C2, 0, uintptr(unsafe.Pointer(insert))) // EM_REPLACESEL
	runtime.KeepAlive(insert)
	procSendMessage.Call(control, 0x00CF, 1, 0) // EM_SETREADONLY on
	procSendMessage.Call(control, 0x00B1,
		uintptr(meetingcore.MapSelection(start, change)),
		uintptr(meetingcore.MapSelection(end, change)))
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
	buttons := meetingcore.ButtonView{Paused: p.paused, CapturePaused: view.capturePaused, CaptionsOnly: p.captionsOnly, ExpressionExpanded: p.expressionExpanded, Pending: view.reply.PendingAvailable}
	p.setText(p.status, meetingcore.StatusText(view.sourceName, view.status, view.metrics.Stage, view.metrics.Dropped, view.capturePaused, p.paused), false)
	p.setText(p.buttons[0], meetingcore.ButtonFace(meetingcore.ButtonPause, buttons), false)
	p.setText(p.buttons[7], meetingcore.ButtonFace(meetingcore.ButtonToggleCapture, buttons), false)
	p.setText(p.buttons[1], meetingcore.ButtonFace(meetingcore.ButtonCaptions, buttons), false)
	p.setText(p.buttons[15], meetingcore.ButtonFace(meetingcore.ButtonExpression, buttons), false)
	for _, i := range []int{6, 7, 8} {
		meetingEnableWindow.Call(p.buttons[i], meetingBool(p.owner != 0))
	}
	meetingEnableWindow.Call(p.buttons[12], meetingBool(p.owner != 0 && view.reply.PendingAvailable && !p.paused))
	p.setText(p.buttons[12], meetingcore.ButtonFace(meetingcore.ButtonAcceptReplies, buttons), false)
	if p.paused {
		return
	}
	panels := meetingcore.BuildReadingPanels(view.captions)
	newTurn := panels.CurrentID != p.currentID || panels.HistoryCount > p.historyCount
	p.currentID = panels.CurrentID
	p.setText(p.live, meetingcore.LiveCaption(panels.Hint), false)
	if meetingcore.TextDue(p.lastTranscript, time.Now(), newTurn || !panels.Live) {
		p.setText(p.currentEnglish, panels.English, true)
		p.setText(p.currentChinese, panels.Chinese, true)
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
	if panels.History != p.rendered[p.captions] {
		p.setText(p.captions, panels.History, true)
		p.unreadHistory = meetingcore.UnreadHistory(p.unreadHistory, p.historyCount, panels.HistoryCount, p.followLatest)
		p.historyCount = panels.HistoryCount
	}
	if p.followLatest {
		p.unreadHistory = 0
	}
	p.setText(p.labels[3], meetingcore.HistoryHeading(panels.HistoryCount, p.unreadHistory), false)
	p.displayedReplies, p.displayedTip = view.reply.Displayed.Lines, view.reply.Displayed.Tip
	p.renderReply()
	newEnglish := view.expression.complete && view.expression.english != "" && view.expression.english != p.copyExpression
	captionsOnly, expanded := meetingcore.ExpressionFocus(view.expression.complete, newEnglish, p.captionsOnly, p.expressionExpanded)
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
	p.setText(p.labels[2], meetingcore.ExpressionHeading(view.expressionStatus, view.expression.complete), false)
	if meetingcore.TextDue(p.lastExpression, time.Now(), view.expression.complete) {
		p.setText(p.expression, meetingcore.ExpressionShown(expression), true)
		if newEnglish {
			procSendMessage.Call(p.expression, 0x0115, 6, 0) // WM_VSCROLL / SB_TOP
		}
		p.lastExpression = time.Now()
	}
	original := meetingcore.ExpressionDetail(view.expressionStatus, view.expressionSource)
	p.setText(p.expressionSource, original, true)
	meetingEnableWindow.Call(p.buttons[5], meetingBool(p.copyExpression != ""))
}

func (p *meetingHUD) renderReply() {
	reply := p.displayedReplies[p.selectedReply]
	for i := 0; i < 3; i++ {
		p.setText(p.buttons[9+i], meetingcore.ButtonFace(meetingcore.ButtonReplyFirst+i, meetingcore.ButtonView{SelectedReply: p.selectedReply}), false)
	}
	text := meetingcore.ReplyBody(reply.English, reply.Chinese, p.displayedTip, reply.Complete)
	if meetingcore.TextDue(p.lastReply, time.Now(), reply.Complete) {
		p.setText(p.responses, text, true)
		p.lastReply = time.Now()
	}
	canCopy := meetingcore.ReplyCopyEnabled(reply.Complete, reply.English)
	meetingEnableWindow.Call(p.buttons[4], meetingBool(canCopy))
	meetingEnableWindow.Call(p.buttons[13], meetingBool(p.owner != 0 && canCopy && !p.paused))
}

func (p *meetingHUD) session() *meetingSession {
	if p == nil || p.app == nil {
		return nil
	}
	return p.app.meeting
}

func (p *meetingHUD) savePreferences() {
	var bounds rect32
	if ok, _, _ := popupGetWindowRect.Call(p.window, uintptr(unsafe.Pointer(&bounds))); ok == 0 {
		return
	}
	// Do not persist minimized or maximized geometry as a normal window size.
	minimized, _, _ := user32.NewProc("IsIconic").Call(p.window)
	maximized, _, _ := user32.NewProc("IsZoomed").Call(p.window)
	prefs, _ := meetingcore.LoadPreferences(meetingPreferencesPath())
	prefs.FontSize, prefs.CaptionsOnly, prefs.Panes = p.fontSize, p.captionsOnly, p.paneWeights
	if minimized == 0 && maximized == 0 {
		prefs.X, prefs.Y, prefs.Width, prefs.Height = bounds.Left, bounds.Top, bounds.Right-bounds.Left, bounds.Bottom-bounds.Top
	}
	if err := meetingcore.SavePreferences(meetingPreferencesPath(), prefs); err != nil && p.app != nil {
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
			w, h := desktop.Fit(p.px(600), p.px(720), work, 0)
			info.MinSize = trayPoint{X: w, Y: h}
			return 0

		case uxWMDrawItem:
			if lParam != 0 {
				item := (*drawItemStruct)(unsafe.Pointer(lParam))
				if item.CtlID >= meetingcore.ButtonPause && item.CtlID <= meetingcore.ButtonExpression {
					uiDrawActionButton(item, p.palette, p.labelFont, uxWinText(p.buttons[item.CtlID-meetingcore.ButtonPause]), meetingcore.ButtonPrimary(int(item.CtlID), p.latest.reply.PendingAvailable, p.expressionExpanded))
					return 1
				}
				if item.CtlID == meetingcore.ButtonSaveProfile || item.CtlID == meetingcore.ButtonCloseProfile {
					uiDrawActionButton(item, p.palette, p.labelFont, uxWinText(item.HwndItem), meetingcore.ButtonPrimary(int(item.CtlID), false, false))
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
			switch action, index := meetingcore.CommandFor(int(wParam & 0xffff)); action {
			case meetingcore.CommandTogglePause:
				p.paused = !p.paused
				updateMeetingHUD(p.latest)
			case meetingcore.CommandCaptions:
				p.captionsOnly = !p.captionsOnly
				p.layout()
				p.savePreferences()
				updateMeetingHUD(p.latest)
			case meetingcore.CommandSmallerText:
				p.resizeText(-2)
			case meetingcore.CommandLargerText:
				p.resizeText(2)
			case meetingcore.CommandCopyReply, meetingcore.CommandCopyExpression:
				reply := p.displayedReplies[p.selectedReply]
				value := meetingcore.CopyText(action, reply.English, reply.Complete, p.copyExpression)
				if value != "" {
					if err := setClipboardText(hwnd, value); err != nil {
						winErr("复制失败: " + err.Error())
					}
				}
			case meetingcore.CommandRequestReplies:
				if session := p.session(); session != nil {
					session.requestReplies()
				}
			case meetingcore.CommandToggleCapture:
				if session := p.session(); session != nil {
					session.setCapturePaused(!p.latest.capturePaused)
					p.app.refreshMeeting()
				}
			case meetingcore.CommandOpenProfile:
				p.openProfile()
			case meetingcore.CommandSelectReply:
				p.selectedReply = index
				p.lastReply = time.Time{}
				p.renderReply()
			case meetingcore.CommandScrollLatest:
				p.scrollToLatest()
			case meetingcore.CommandToggleExpression:
				p.expressionExpanded = !p.expressionExpanded
				p.layout()
				updateMeetingHUD(p.latest)
			case meetingcore.CommandAcceptReplies:
				if session := p.session(); session != nil && !p.paused {
					session.acceptPendingReplies()
					p.app.refreshMeeting()
				}
			case meetingcore.CommandRefine:
				reply := p.displayedReplies[p.selectedReply]
				instruction := strings.TrimSpace(uxWinText(p.instruction))
				if session := p.session(); session != nil && meetingcore.RefineEnabled(reply.Complete, instruction, p.paused) {
					session.refineReply(p.selectedReply, instruction)
				}
			case meetingcore.CommandSaveProfile:
				p.saveProfile()
			case meetingcore.CommandCloseProfile:
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
		captions: []meetingcore.Caption{
			{Final: true, Source: "The impact seems to be limited to peak hours.", Chinese: "影响似乎仅限于高峰时段。"},
			{Final: true, Source: "We're seeing high tail latency on vector search. How would you start investigating?", Chinese: "向量检索的尾延迟偏高。你会从哪里开始排查？"},
		},
		reply: meetingcore.ReplyState{Displayed: meetingcore.ReplyBatch{
			Lines: [3]meetingcore.Line{
				{Kind: "ONE", Complete: true, English: "I'd start by separating time spent waiting in the queue from time spent running the search.", Chinese: "我会先把排队耗时和实际检索耗时分开看。"},
				{Kind: "TWO", Complete: true, English: "Is the spike limited to certain queries, or does it affect the whole cluster?", Chinese: "是部分查询出现尖峰，还是整个集群都受影响？"},
				{Kind: "THREE", Complete: true, English: "Let me take a moment to think through where we'd get the clearest signal.", Chinese: "我先理一下从哪里入手能最快弄清情况。"},
			},
			Source: "We're seeing high tail latency on vector search. How would you start investigating?",
			Tip:    "先对照高峰期的 p99 排队时间与分片耗时，再核对是否集中在特定索引或查询类型。",
		}},
		expression: meetingLine{kind: "SAY", complete: true, english: "We can do that, but by Friday I can only share an initial version.", chinese: "可以做，但周五前我只能给初版。"},
	}
}
