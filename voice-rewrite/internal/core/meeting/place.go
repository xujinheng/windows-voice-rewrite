package meeting

import "voicerewrite/internal/core/desktop"

// Place keeps the meeting window on the work area. A missing size uses the
// default frame at the top-right of that area.
func Place(p Preferences, work desktop.WorkArea, scale float64) (x, y, w, h int32) {
	w, h = p.Width, p.Height
	if w < int32(560*scale) || h < int32(640*scale) {
		w, h = int32(640*scale), int32(720*scale)
	}
	w, h = desktop.Fit(w, h, work, 0)
	x, y = p.X, p.Y
	if p.Width <= 0 || p.Height <= 0 {
		x, y = work.Right-w-12, work.Top+12
	}
	x, y = desktop.Clamp(x, y, w, h, work)
	return
}
