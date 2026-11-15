//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

type meetingHUD struct {
	app                                                         *appState
	window, owner                                               uintptr
	status, live, captions, responses, expression, instruction  uintptr
	labelFont, bodyFont                                         uintptr
	palette                                                     uiPalette
	background, surface                                         uintptr
	labels                                                      [3]uintptr
	buttons                                                     [14]uintptr
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
	p := &meetingHUD{window: hwnd, owner: owner, palette: resultCardPalette(), scale: scale, fontSize: prefs.FontSize, captionsOnly: prefs.CaptionsOnly, rendered: map[uintptr]string{}}
	activeMeetingHUD = p
	p.background, _, _ = procCreateSolidBrush.Call(p.palette.color(p.palette.background))
	p.surface, _, _ = procCreateSolidBrush.Call(p.palette.color(p.palette.surface))
	p.labelFont, p.bodyFont = popupFont(p.px(14), 600), popupFont(p.px(p.fontSize), 400)
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
	for i, title := range []string{"暂停显示", "仅字幕", "A−", "A+", "复制所选", "复制我的英文", "生成建议", "暂停采集", "会议资料", "1 直接回应", "2 澄清一下", "3 争取时间", "切换新建议", "改写所选"} {
		p.buttons[i] = child("BUTTON", title, 0x00010000, uintptr(7100+i), false)
	}
	p.labels[0] = child("STATIC", "实时字幕 · 已确认片段 / 仍在识别", 0, 0, false)
	editStyle := uintptr(WS_VSCROLL | ES_MULTILINE | ES_READONLY | ES_AUTOVSCROLL | WS_BORDER | 0x00010000)
	p.live = child("EDIT", "", editStyle, 0, true)
	p.captions = child("EDIT", "", editStyle, 0, true)
	p.labels[1] = child("STATIC", "回复建议 · 选择 1 / 2 / 3 阅读", 0, 0, false)
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

func (p *meetingHUD) px(v int32) int32 { return int32(float64(v)*p.scale + .5) }
func (p *meetingHUD) move(control uintptr, x, y, w, h int32) {
	procMoveWindow.Call(control, int32ptr(x), int32ptr(y), int32ptr(max(1, w)), int32ptr(max(1, h)), 1)
}
func (p *meetingHUD) layout() {
	var bounds rect32
	procGetClientRect.Call(p.window, uintptr(unsafe.Pointer(&bounds)))
	margin, gap, label, button := p.px(12), p.px(6), p.px(23), p.px(29)
	width := bounds.Right - 2*margin
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
	y := p.px(8)
	p.move(p.status, margin, y, width, p.px(80))
	y += p.px(84)
	x := margin
	for _, item := range []struct {
		index int
		width int32
	}{{0, 84}, {7, 84}, {1, 70}, {2, 32}, {3, 32}, {6, 80}, {8, 80}} {
		p.move(p.buttons[item.index], x, y, p.px(item.width), button)
		x += p.px(item.width) + gap
	}
	y += button + gap
	remaining := bounds.Bottom - y - margin
	captionsHeight := remaining * 42 / 100
	if p.captionsOnly {
		captionsHeight = remaining
	}
	p.move(p.labels[0], margin, y, width, label)
	y += label
	liveHeight := min(p.px(74), max(p.px(40), captionsHeight/3))
	p.move(p.live, margin, y, width, liveHeight)
	y += liveHeight + gap
	historyHeight := max(p.px(36), captionsHeight-label-liveHeight-gap)
	p.move(p.captions, margin, y, width, historyHeight)
	y += historyHeight + gap
	for _, control := range []uintptr{p.labels[1], p.labels[2], p.responses, p.expression, p.instruction, p.buttons[4], p.buttons[5], p.buttons[9], p.buttons[10], p.buttons[11], p.buttons[12], p.buttons[13]} {
		procShowWindow.Call(control, meetingShow(!p.captionsOnly))
	}
	if !p.captionsOnly {
		p.move(p.labels[1], margin, y, width, label)
		y += label
		x = margin
		// Three choices remain visible while the selected reply is read or edited.
		for _, item := range []struct {
			index int
			width int32
		}{{9, 98}, {10, 98}, {11, 98}, {4, 86}, {12, 110}} {
			p.move(p.buttons[item.index], x, y, p.px(item.width), button)
			x += p.px(item.width) + gap
		}
		y += button + gap
		space := bounds.Bottom - y - margin - button - label - 3*gap
		replyHeight := max(p.px(54), space*58/100)
		expressionHeight := max(p.px(38), space-replyHeight)
		p.move(p.responses, margin, y, width, replyHeight)
		y += replyHeight + gap
		p.move(p.instruction, margin, y, width-p.px(98)-gap, button)
		p.move(p.buttons[13], margin+width-p.px(98), y, p.px(98), button)
		y += button + gap
		p.move(p.labels[2], margin, y, width-p.px(130), label)
		p.move(p.buttons[5], margin+width-p.px(126), y, p.px(126), label)
		y += label
		p.move(p.expression, margin, y, width, expressionHeight)
	}
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
	for _, control := range []uintptr{p.live, p.captions, p.responses, p.expression, p.instruction, p.profileBackground, p.profileTerms} {
		if control != 0 {
			setFont(control, font)
		}
	}
	procDeleteObject.Call(old)
	p.savePreferences()
}

// EDIT replaces reset selection and scroll. Snapshot both around history updates;
// live recognition has its own control so token updates never touch history.
func (p *meetingHUD) setText(control uintptr, value string, preserve bool) {
	if value == p.rendered[control] {
		return
	}
	var start, end uint32
	var first uintptr
	if preserve {
		procSendMessage.Call(control, 0x00B0, uintptr(unsafe.Pointer(&start)), uintptr(unsafe.Pointer(&end)))
		first, _, _ = procSendMessage.Call(control, 0x00CE, 0, 0)
		procSendMessage.Call(control, 0x000B, 0, 0)
	}
	uxSetText(control, value)
	if preserve {
		procSendMessage.Call(control, 0x00B1, uintptr(start), uintptr(end))
		now, _, _ := procSendMessage.Call(control, 0x00CE, 0, 0)
		procSendMessage.Call(control, 0x00B6, 0, uintptr(int64(first)-int64(now)))
		procSendMessage.Call(control, 0x000B, 1, 0)
	}
	p.rendered[control] = value
	meetingInvalidateRect.Call(control, 0, 1)
}

func meetingLatency(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	return fmt.Sprintf("%dms", ms)
}

func updateMeetingHUD(view meetingView) {
	p := activeMeetingHUD
	if p == nil {
		return
	}
	p.latest = view
	mode := "multipart 分段识别"
	if view.asrMode == "llama-prefix" {
		mode = "llama 累积音频 / 前缀续写"
	}
	source := view.sourceName
	if source == "" {
		source = "等待音频源"
	}
	source = strings.Join(strings.Fields(source), " ")
	if runes := []rune(source); len(runes) > 36 {
		source = string(runes[:35]) + "…"
	}
	state := view.status
	if p.paused {
		state = "显示已暂停（处理继续） · " + state
	}
	if view.capturePaused {
		state = "采集已暂停 · " + state
	}
	m := view.metrics
	status := fmt.Sprintf("%s\r\n%s · 音量 %d%%\r\n%s · %s · 排队 %d / 丢弃 %d\r\n识别请求 首字/完成 %s / %s · 翻译请求 首字/完成 %s / %s", state, source, view.level, mode, m.Stage, m.Pending, m.Dropped, meetingLatency(m.ASRFirstMS), meetingLatency(m.ASRDoneMS), meetingLatency(m.TranslationFirstMS), meetingLatency(m.TranslationDoneMS))
	p.setText(p.status, status, false)
	p.setText(p.buttons[0], map[bool]string{false: "暂停显示", true: "恢复显示"}[p.paused], false)
	p.setText(p.buttons[7], map[bool]string{false: "暂停采集", true: "恢复采集"}[view.capturePaused], false)
	p.setText(p.buttons[1], map[bool]string{false: "仅字幕", true: "完整视图"}[p.captionsOnly], false)
	for _, i := range []int{6, 7, 8} {
		meetingEnableWindow.Call(p.buttons[i], meetingBool(p.owner != 0))
	}
	meetingEnableWindow.Call(p.buttons[12], meetingBool(p.owner != 0 && view.replyPending && !p.paused))
	p.setText(p.buttons[12], map[bool]string{false: "暂无新建议", true: "切换新建议"}[view.replyPending], false)
	meetingEnableWindow.Call(p.buttons[13], meetingBool(p.owner != 0 && p.displayedReplies[p.selectedReply].complete && p.displayedReplies[p.selectedReply].english != "" && !p.paused))
	if p.paused {
		return
	}
	var history, live []string
	for _, line := range view.captions {
		translation := line.chinese
		if translation == "" {
			translation = "正在翻译…"
		}
		if line.final {
			history = append(history, translation+"\r\n"+line.source)
			continue
		}
		text := "已确认 · " + line.stable
		if line.live != "" {
			text += "\r\n识别中 · " + line.live
		}
		if line.stable == "" && line.live == "" {
			text = "识别中 · " + line.source
		}
		live = append(live, text+"\r\n"+translation)
	}
	if len(live) == 0 {
		live = append(live, "等待对方说话 · 实时识别显示在这里。")
	}
	if len(history) == 0 {
		history = append(history, "已结束的字幕保留在这里，可选择复制。")
	}
	p.setText(p.live, strings.Join(live, "\r\n\r\n"), true)
	p.setText(p.captions, strings.Join(history, "\r\n\r\n"), true)
	p.displayedReplies, p.displayedSource, p.displayedTip = view.replies, view.replySource, view.tip
	p.renderReply()
	expression := view.expressionStatus
	p.copyExpression = ""
	if expression == "" && view.expression.english != "" {
		expression = view.expression.english + "\r\n" + view.expression.chinese
		if view.expression.complete {
			p.copyExpression = view.expression.english
		} else {
			expression += "\r\n正在生成…（完成后可复制）"
		}
	}
	if expression == "" {
		expression = "按住听写键说中文，松开后显示英文。"
	}
	p.setText(p.expression, expression, true)
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
		text = "听到提问后生成建议；新建议需点击“切换新建议”后阅读。"
	}
	if p.displayedSource != "" {
		text += "\r\n\r\n对应原话 · " + p.displayedSource
	}
	if p.displayedTip != "" {
		text += "\r\n技术提示 · " + p.displayedTip
	}
	p.setText(p.responses, text, true)
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
				p.renderReply()
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
			procSetTextColor.Call(wParam, p.palette.color(p.palette.text))
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
