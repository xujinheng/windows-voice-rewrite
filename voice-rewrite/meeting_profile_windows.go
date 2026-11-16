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
		child("STATIC", meetingcore.ProfileTitle, 0, 0, false)
		child("STATIC", meetingcore.ProfileBackground, 0, 0, false)
		editStyle := uintptr(WS_VSCROLL | ES_MULTILINE | ES_AUTOVSCROLL | WS_BORDER | 0x00010000 | 0x1000)
		p.profileBackground = child("EDIT", "", editStyle, 7201, true)
		procSendMessage.Call(p.profileBackground, 0x00C5, uintptr(meetingcore.MaxBackgroundRunes), 0)
		child("STATIC", meetingcore.ProfileTerms, 0, 0, false)
		p.profileTerms = child("EDIT", "", editStyle, 7202, true)
		procSendMessage.Call(p.profileTerms, 0x00C5, uintptr(meetingcore.MaxTermsRunes), 0)
		child("STATIC", meetingcore.ProfileMode, 0, 0, false)
		p.profileMode = child("COMBOBOX", "", 0x00010000|WS_VSCROLL|3, 7203, false)
		for _, mode := range config.MeetingASRModes() {
			title := mode + " · " + config.MeetingASRModeTitle(mode)
			ptr, _ := syscall.UTF16PtrFromString(title)
			procSendMessage.Call(p.profileMode, uxCBAddString, 0, uintptr(unsafe.Pointer(ptr)))
			runtime.KeepAlive(ptr)
		}
		child("STATIC", config.MeetingASRModeNote(), 0, 0, false)
		p.profileStatus = child("STATIC", "", 0, 0, false)
		child("BUTTON", meetingcore.ButtonFace(meetingcore.ButtonSaveProfile, meetingcore.ButtonView{}), 0x00010000|uxBSOwnerDraw, meetingcore.ButtonSaveProfile, false)
		child("BUTTON", meetingcore.ButtonFace(meetingcore.ButtonCloseProfile, meetingcore.ButtonView{}), 0x00010000|uxBSOwnerDraw, meetingcore.ButtonCloseProfile, false)
		if err != nil {
			for _, control := range p.profileControls {
				if control != 0 {
					procDestroyWindow.Call(control)
				}
			}
			p.profileControls = nil
			p.profileBackground, p.profileTerms, p.profileMode, p.profileStatus = 0, 0, 0, 0
			winErr(meetingcore.ProfileOpenFailed + err.Error())
			return
		}
	}
	session := p.app.meeting
	session.mu.Lock()
	cfg := session.cfg
	session.mu.Unlock()
	uxSetText(p.profileBackground, cfg.MeetingBackground)
	uxSetText(p.profileTerms, cfg.MeetingTerms)
	procSendMessage.Call(p.profileMode, uxCBSetCurSel, uintptr(config.MeetingASRModeIndex(cfg.MeetingASRMode)), 0)
	uxSetText(p.profileStatus, "")
	p.profileOpen = true
	p.layout()
	meetingSetFocus.Call(p.profileBackground)
}

func (p *meetingHUD) layoutProfile(width, height int32) {
	if len(p.profileControls) != 11 {
		return
	}
	boxes := meetingcore.ProfileLayout(width, height, meetingcore.ProfileMetrics{
		Margin: p.px(12), Gap: p.px(8), Label: p.px(25), Chrome: p.px(246), MinEdit: p.px(70),
		ModeHeight: p.px(180), ModeStep: p.px(34), NoteHeight: p.px(48), ButtonSize: p.px(32),
		SaveWidth: p.px(130), CloseOffset: p.px(140), CloseWidth: p.px(96),
	})
	for i, box := range boxes {
		p.move(p.profileControls[i], box.X, box.Y, box.W, box.H)
	}
	meetingInvalidateRect.Call(p.window, 0, 1)
}

func (p *meetingHUD) saveProfile() {
	s := p.session()
	if s == nil {
		return
	}
	if err := s.updateProfile(uxWinText(p.profileBackground), uxWinText(p.profileTerms)); err != nil {
		uxSetText(p.profileStatus, meetingcore.ProfileSaveFailed+err.Error())
		return
	}
	index, _, _ := procSendMessage.Call(p.profileMode, uxCBGetCurSel, 0, 0)
	mode := config.MeetingASRModeAt(int(index))
	if err := s.setASRMode(mode); err != nil {
		uxSetText(p.profileStatus, meetingcore.ProfileModeFailed+err.Error())
		return
	}
	p.profileOpen = false
	p.layout()
	p.app.refreshMeeting()
	meetingSetFocus.Call(p.buttons[8])
}
