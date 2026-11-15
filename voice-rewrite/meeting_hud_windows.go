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
	currentEnglish, currentChinese                                uintptr
	labelFont, bodyFont, englishFont, chineseFont                uintptr
	palette                                                     uiPalette
	background, surface                                         uintptr
	labels                                                      [4]uintptr
	buttons                                                     [15]uintptr
	mainControls                                                []uintptr
	profileControls                                             []uintptr
	profileBackground, profileTerms, profileMode, profileStatus uintptr
	scale                                                       float64
	fontSize                                                    int32
	paused, captionsOnly, profileOpen                           bool
	latest                                                      meetingView
	displayedReplies                                            [3]meetingLine
	displayedSource, displayedTip, copyExpression               string
	selectedReply                                               int
	followLatest                                                bool
	transcriptDrawn                                             bool
	transcriptFirstLine                                         uintptr
	historyCount, unreadHistory                                 int
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
	p := &meetingHUD{window: hwnd, owner: owner, palette: resultCardPalette(), scale: scale, fontSize: prefs.FontSize, captionsOnly: prefs.CaptionsOnly, followLatest: true, rendered: map[uintptr]string{}}
	activeMeetingHUD = p
	p.background, _, _ = procCreateSolidBrush.Call(p.palette.color(p.palette.background))
	p.surface, _, _ = procCreateSolidBrush.Call(p.palette.color(p.palette.surface))
	p.labelFont = meetingUIFont(p.px(14), 600)
	p.bodyFont = meetingUIFont(p.px(p.fontSize), 400)
	p.englishFont = popupFont(-p.px(p.fontSize+4), 500)
	p.chineseFont = meetingUIFont(p.px(p.fontSize+2), 500)
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
	for i, title := range []string{"冻结画面", "专注字幕", "A−", "A+", "复制回复", "复制英文", "生成建议", "暂停采集", "会议资料", "1 直接回应", "2 澄清一下", "3 争取时间", "查看新建议", "改写回复", "回到最新"} {
		p.buttons[i] = child("BUTTON", title, 0x00010000|uxBSOwnerDraw, uintptr(7100+i), false)
	}
	p.labels[0] = child("STATIC", "当前发言", 0, 0, false)
	editStyle := uintptr(WS_VSCROLL | ES_MULTILINE | ES_READONLY | ES_AUTOVSCROLL | WS_BORDER | 0x00010000)
	p.live = child("STATIC", "● 等待发言", 0, 0, false)
	currentStyle := uintptr(ES_MULTILINE | ES_READONLY | ES_AUTOVSCROLL | WS_VSCROLL)
	p.currentEnglish = child("EDIT", "", currentStyle, 0, false)
	p.currentChinese = child("EDIT", "", currentStyle, 0, false)
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

	// Current speech never belongs to the scrolling history. EN and ZH are
	// independent reading areas, so translation updates cannot reflow EN.
	englishHeight, chineseHeight := p.px(76), p.px(64)
	if bounds.Bottom < p.px(760) {
		englishHeight, chineseHeight = p.px(68), p.px(56)
	}
	p.move(p.currentEnglish, margin, y, width, englishHeight)
	y += englishHeight + p.px(2)
	p.move(p.currentChinese, margin, y, width, chineseHeight)
	y += chineseHeight + gap
	p.move(p.labels[3], margin, y, width-p.px(114), label)
	p.move(p.buttons[14], margin+width-p.px(110), y-p.px(3), p.px(110), button)
	y += label + gap

	// The user's generated utterance is anchored to the bottom of the window.
	// Only the history area expands when the meeting window grows.
	footer := bounds.Bottom - margin
	for _, control := range []uintptr{p.labels[1], p.labels[2], p.responses, p.expression, p.instruction, p.buttons[4], p.buttons[5], p.buttons[6], p.buttons[9], p.buttons[10], p.buttons[11], p.buttons[12], p.buttons[13]} {
		procShowWindow.Call(control, meetingShow(!p.captionsOnly))
	}
	if p.captionsOnly {
		p.move(p.captions, margin, y, width, max(p.px(60), footer-y))
		meetingInvalidateRect.Call(p.window, 0, 1)
		return
	}

	replyHeight, expressionHeight := p.px(70), p.px(80)
	if bounds.Bottom > p.px(950) {
		replyHeight, expressionHeight = p.px(108), p.px(112)
	}
	expressionHeader := footer - expressionHeight - label
	p.move(p.labels[2], margin, expressionHeader, width-p.px(116), label)
	p.move(p.buttons[5], margin+width-p.px(112), expressionHeader-p.px(4), p.px(112), button)
	p.move(p.expression, margin, expressionHeader+label, width, expressionHeight)

	instructionY := expressionHeader - gap - button
	p.move(p.instruction, margin, instructionY, width-p.px(112)-gap, button)
	p.move(p.buttons[13], margin+width-p.px(112), instructionY, p.px(112), button)
	replyY := instructionY - gap - replyHeight
	p.move(p.responses, margin, replyY, width, replyHeight)
	optionsY := replyY - gap - button
	x = margin
	for _, item := range []struct {
		index int
		width int32
	}{{9, 112}, {10, 112}, {11, 112}} {
		p.move(p.buttons[item.index], x, optionsY, p.px(item.width), button)
		x += p.px(item.width) + gap
	}
	replyTitleY := optionsY - gap - button
	p.move(p.labels[1], margin, replyTitleY, width-p.px(316), button)
	p.move(p.buttons[6], margin+width-p.px(310), replyTitleY, p.px(92), button)
	p.move(p.buttons[12], margin+width-p.px(210), replyTitleY, p.px(108), button)
	p.move(p.buttons[4], margin+width-p.px(94), replyTitleY, p.px(94), button)

	p.move(p.captions, margin, y, width, max(p.px(48), replyTitleY-gap-y))
	meetingInvalidateRect.Call(p.window, 0, 1)
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
	font := popupFont(p.px(size), 400)
	if font == 0 {
		return
	}
	old := p.bodyFont
	p.bodyFont, p.fontSize = font, size
	for _, control := range []uintptr{p.captions, p.responses, p.expression, p.instruction, p.profileBackground, p.profileTerms} {
		if control != 0 {
			setFont(control, font)
		}
	}
	procDeleteObject.Call(old)
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
	// EM_LINESCROLL moves the viewport, not the selection/caret.
	procSendMessage.Call(p.captions, 0x00B6, 0, uintptr(1<<20))
	p.transcriptFirstLine, _, _ = procSendMessage.Call(p.captions, 0x00CE, 0, 0)
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
	for _, i := range []int{6, 7, 8} {
		meetingEnableWindow.Call(p.buttons[i], meetingBool(p.owner != 0))
	}
	meetingEnableWindow.Call(p.buttons[12], meetingBool(p.owner != 0 && view.replyPending && !p.paused))
	p.setText(p.buttons[12], map[bool]string{false: "无新建议", true: "查看新建议"}[view.replyPending], false)
	if p.paused {
		return
	}
	transcript, live := meetingTranscript(view.captions)
	activity := "● 已同步"
	if live {
		activity = "● 正在识别"
	} else if view.capturePaused {
		activity = "● 已暂停"
	} else if len(view.captions) == 0 {
		activity = "● 等待发言"
	}
	p.setText(p.live, activity, false)
	if !live || time.Since(p.lastTranscript) >= 280*time.Millisecond {
		p.setText(p.captions, transcript, true)
		p.lastTranscript = time.Now()
	}
	p.displayedReplies, p.displayedSource, p.displayedTip = view.replies, view.replySource, view.tip
	p.renderReply()
	expression := view.expressionStatus
	p.copyExpression = ""
	if expression == "" && view.expression.english != "" {
		expression = view.expression.english
		if view.expression.chinese != "" {
			expression += "\r\n" + view.expression.chinese
		}
		if view.expressionSource != "" {
			expression += "\r\n\r\n语音原文 · " + view.expressionSource
		}
		if view.expression.complete {
			p.copyExpression = view.expression.english
		} else {
			expression += "\r\n正在生成…"
		}
	}
	if expression == "" {
		expression = "会议中：按听写键直接翻成英文；按改写键说你的意图，生成可直接说出的英文。"
	}
	if view.expression.complete || time.Since(p.lastExpression) >= 280*time.Millisecond {
		p.setText(p.expression, expression, true)
		p.lastExpression = time.Now()
	}
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
	if p.displayedSource != "" {
		text += "\r\n\r\n对应原话 · " + p.displayedSource
	}
	if p.displayedTip != "" {
		text += "\r\n技术提示 · " + p.displayedTip
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
	prefs.FontSize, prefs.CaptionsOnly = p.fontSize, p.captionsOnly
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
	for _, handle := range []uintptr{p.labelFont, p.bodyFont, p.background, p.surface} {
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
			w, h := desktopFitSize(p.px(600), p.px(640), work, 0)
			info.MinSize = trayPoint{X: w, Y: h}
			return 0

		case uxWMDrawItem:
			if lParam != 0 {
				item := (*drawItemStruct)(unsafe.Pointer(lParam))
				if item.CtlID >= 7100 && item.CtlID <= 7114 {
					uiDrawActionButton(item, p.palette, p.labelFont, uxWinText(p.buttons[item.CtlID-7100]), item.CtlID == 7106 || item.CtlID == 7112 && p.latest.replyPending)
					return 1
				}
				if item.CtlID == 7205 || item.CtlID == 7206 {
					uiDrawActionButton(item, p.palette, p.labelFont, uxWinText(item.HwndItem), item.CtlID == 7205)
					return 1
				}
			}
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
			if lParam == p.captions || lParam == p.live || lParam == p.responses || lParam == p.expression || lParam == p.instruction || lParam == p.profileBackground || lParam == p.profileTerms {
				procSetBkColor.Call(wParam, p.palette.color(p.palette.surface))
				return p.surface
			}
			procSetBkColor.Call(wParam, p.palette.color(p.palette.background))
			return p.background
		case WM_ERASEBKGND:
			var bounds rect32
			procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
			procFillRect.Call(wParam, uintptr(unsafe.Pointer(&bounds)), p.background)
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
