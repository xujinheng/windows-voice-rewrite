package main

import meetingcore "voicerewrite/internal/core/meeting"

func meetingGeometry(p meetingcore.Preferences, work desktopWorkArea, scale float64) (x, y, w, h int32) {
	w, h = p.Width, p.Height
	if w < int32(560*scale) || h < int32(640*scale) {
		w, h = int32(640*scale), int32(720*scale)
	}
	w, h = desktopFitSize(w, h, work, 0)
	x, y = p.X, p.Y
	if p.Width <= 0 || p.Height <= 0 {
		x, y = work.right-w-12, work.top+12
	}
	x, y = desktopClampPosition(x, y, w, h, work)
	return
}
