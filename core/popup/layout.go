// Package popup sizes a result card and decides which clicks dismiss it.
// Hosts own the native window and its buttons.
package popup

type HitRect struct{ Left, Top, Right, Bottom int32 }

func (r HitRect) Contains(x, y int32) bool {
	return r.Right > r.Left && r.Bottom > r.Top &&
		x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

// SizeForText grows only the reading area. The width stays fixed.
func SizeForText(runes int) (width, height int32) {
	width, height = 476, 258
	switch {
	case runes > 1200:
		height = 500
	case runes > 600:
		height = 430
	case runes > 200:
		height = 315
	}
	return
}

// ClickDismisses is true outside the card, and inside it except on the
// response text and the action buttons.
func ClickDismisses(x, y int32, window, editor HitRect, buttons []HitRect) bool {
	if !window.Contains(x, y) {
		return true
	}
	if editor.Contains(x, y) {
		return false
	}
	for _, button := range buttons {
		if button.Contains(x, y) {
			return false
		}
	}
	return true
}
