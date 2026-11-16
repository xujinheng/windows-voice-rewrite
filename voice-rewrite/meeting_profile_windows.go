//go:build windows

package main

import (
	"runtime"
	"syscall"
	"unsafe"

	"voicerewrite/internal/core/config"
	meetingcore "voicerewrite/internal/core/meeting"
)

func (p *meetingHUD) openProfile() {
	if p.app == nil || p.app.meeting == nil {
		return
	}
	if len(p.profileControls) == 0 {
		var err error
		child := func(class, text string, style, id uintptr, body bool) uintptr {
			if err != nil {
				return 0
			}
			var control uintptr
			control, err = makeChild(class, text, style|WS_CHILD, 0, 0, 1, 1, p.window, id)
			font := p.labelFont
			if body {
				font = p.bodyFont
			}
			setFont(control, font)
			p.profileControls = append(p.profileControls, control)
			return control
		}
		child("STATIC", "会议资料 · 保存后立即用于本次会议和下次启动", 0, 0, false)
		child("STATIC", "背景：角色、项目、目标、已知限制", 0, 0, false)
		editStyle := uintptr(WS_VSCROLL | ES_MULTILINE | ES_AUTOVSCROLL | WS_BORDER | 0x00010000 | 0x1000)
		p.profileBackground = child("EDIT", "", editStyle, 7201, true)
		procSendMessage.Call(p.profileBackground, 0x00C5, uintptr(meetingcore.MaxBackgroundRunes), 0)
		child("STATIC", "术语：人名、缩写、产品名和中英对照（每行一条）", 0, 0, false)
		p.profileTerms = child("EDIT", "", editStyle, 7202, true)
		procSendMessage.Call(p.profileTerms, 0x00C5, uintptr(meetingcore.MaxTermsRunes), 0)
		child("STATIC", "会议识别方式", 0, 0, false)
		p.profileMode = child("COMBOBOX", "", 0x00010000|WS_VSCROLL|3, 7203, false)
		labels := map[string]string{
			config.MeetingASRMultipart:   "分段音频识别",
			config.MeetingASRLlamaPrefix: "累积音频 / 前缀续写",
		}
		for _, mode := range config.MeetingASRModes() {
			title := mode + " · " + labels[mode]
			ptr, _ := syscall.UTF16PtrFromString(title)
			procSendMessage.Call(p.profileMode, uxCBAddString, 0, uintptr(unsafe.Pointer(ptr)))
			runtime.KeepAlive(ptr)
		}
		child("STATIC", config.MeetingASRLlamaPrefix+" 重复提交累积音频并延续文本前缀；不是原生编码器流式识别。需服务支持该协议。保存会取消当前处理并重新开始。", 0, 0, false)
		p.profileStatus = child("STATIC", "", 0, 0, false)
		child("BUTTON", "保存并应用", 0x00010000|uxBSOwnerDraw, meetingcore.ButtonSaveProfile, false)
		child("BUTTON", "取消", 0x00010000|uxBSOwnerDraw, meetingcore.ButtonCloseProfile, false)
		if err != nil {
			for _, control := range p.profileControls {
				if control != 0 {
					procDestroyWindow.Call(control)
				}
			}
			p.profileControls = nil
			p.profileBackground, p.profileTerms, p.profileMode, p.profileStatus = 0, 0, 0, 0
			winErr("打开会议资料失败: " + err.Error())
			return
		}
	}
	session := p.app.meeting
	session.mu.Lock()
	cfg := session.cfg
	session.mu.Unlock()
	uxSetText(p.profileBackground, cfg.MeetingBackground)
	uxSetText(p.profileTerms, cfg.MeetingTerms)
	index := uintptr(0)
	for i, mode := range config.MeetingASRModes() {
		if cfg.MeetingASRMode == mode {
			index = uintptr(i)
			break
		}
	}
	procSendMessage.Call(p.profileMode, uxCBSetCurSel, index, 0)
	uxSetText(p.profileStatus, "")
	p.profileOpen = true
	p.layout()
	meetingSetFocus.Call(p.profileBackground)
}

func (p *meetingHUD) layoutProfile(width, height int32) {
	if len(p.profileControls) != 11 {
		return
	}
	margin, gap, label := p.px(12), p.px(8), p.px(25)
	y := p.px(12)
	p.move(p.profileControls[0], margin, y, width, label*2)
	y += label*2 + gap
	available := height - y - margin - p.px(246)
	editHeight := max(p.px(70), available/2)
	p.move(p.profileControls[1], margin, y, width, label)
	y += label
	p.move(p.profileBackground, margin, y, width, editHeight)
	y += editHeight + gap
	p.move(p.profileControls[3], margin, y, width, label)
	y += label
	p.move(p.profileTerms, margin, y, width, editHeight)
	y += editHeight + gap
	p.move(p.profileControls[5], margin, y, width, label)
	y += label
	p.move(p.profileMode, margin, y, width, p.px(180))
	y += p.px(34)
	p.move(p.profileControls[7], margin, y, width, p.px(48))
	y += p.px(48) + gap
	p.move(p.profileStatus, margin, y, width, label)
	y += label + gap
	p.move(p.profileControls[9], margin, y, p.px(130), p.px(32))
	p.move(p.profileControls[10], margin+p.px(140), y, p.px(96), p.px(32))
	meetingInvalidateRect.Call(p.window, 0, 1)
}

func (p *meetingHUD) saveProfile() {
	if p.app == nil || p.app.meeting == nil {
		return
	}
	s := p.app.meeting
	if err := s.updateProfile(uxWinText(p.profileBackground), uxWinText(p.profileTerms)); err != nil {
		uxSetText(p.profileStatus, "保存失败: "+err.Error())
		return
	}
	index, _, _ := procSendMessage.Call(p.profileMode, uxCBGetCurSel, 0, 0)
	mode := config.MeetingASRMultipart
	if modes := config.MeetingASRModes(); index < uintptr(len(modes)) {
		mode = modes[index]
	}
	if err := s.setASRMode(mode); err != nil {
		uxSetText(p.profileStatus, "资料已保存；识别方式保存失败: "+err.Error())
		return
	}
	p.profileOpen = false
	p.layout()
	p.app.refreshMeeting()
	meetingSetFocus.Call(p.buttons[8])
}
