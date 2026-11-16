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

// Rect is a control frame in the same pixels as the card client area.
type Rect struct{ X, Y, W, H int32 }

func scaled(v int32, scale float64) int32 {
	if scale <= 0 {
		scale = 1
	}
	return int32(float64(v)*scale + 0.5)
}

// Editor is the read-only result body. Width and height stay inset from the client edge.
func Editor(clientW, clientH int32, scale float64) Rect {
	return Rect{scaled(22, scale), scaled(59, scale), clientW - scaled(44, scale), clientH - scaled(119, scale)}
}

// Note is the slim status line shown after copy or apply.
func Note(clientW int32, scale float64) Rect {
	return Rect{scaled(132, scale), scaled(16, scale), clientW - scaled(190, scale), scaled(20, scale)}
}

// CloseButton sits in the header, inset from the right edge.
func CloseButton(clientW int32, scale float64) Rect {
	return Rect{clientW - scaled(51, scale), scaled(11, scale), scaled(31, scale), scaled(29, scale)}
}

// CopyButton is the bottom-right action.
func CopyButton(clientW, clientH int32, scale float64) Rect {
	return Rect{clientW - scaled(119, scale), clientH - scaled(48, scale), scaled(98, scale), scaled(33, scale)}
}

// ApplyButton sits to the left of Copy when the result can still be written.
func ApplyButton(clientW, clientH int32, scale float64) Rect {
	return Rect{clientW - scaled(243, scale), clientH - scaled(48, scale), scaled(116, scale), scaled(33, scale)}
}
