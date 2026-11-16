// Package settingsform is the logical-pixel geometry of the settings window.
// Hosts scale it to the monitor DPI when creating native controls.
package settingsform

const (
	DesignWidth   int32 = 816
	DesignHeight  int32 = 484
	MinWidth            = 480
	MinHeight           = 420
	MinWorkMargin       = 12
)

type Spec struct {
	X, Y, W, H     int32
	Stretch, Right bool
}

// Layout stretches or right-anchors a control when the window is wider than
// the design width. Narrower windows keep the design coordinates.
func Layout(c Spec, clientWidth int32) (x, y, w, h int32) {
	delta := clientWidth - DesignWidth
	if delta < 0 {
		delta = 0
	}
	x, y, w, h = c.X, c.Y, c.W, c.H
	if c.Right {
		x += delta
	}
	if c.Stretch {
		w += delta
	}
	return
}

// MinimumSize is the settings window's minimum client size at the monitor scale.
// A small work area keeps the window inside the display, leaving the same margin.
func MinimumSize(scale float64, workWidth, workHeight int32) (int32, int32) {
	minW := int32(MinWidth * scale)
	minH := int32(MinHeight * scale)
	if minW > workWidth-MinWorkMargin {
		minW = workWidth - MinWorkMargin
	}
	if minH > workHeight-MinWorkMargin {
		minH = workHeight - MinWorkMargin
	}
	return minW, minH
}

// ClampScroll keeps a scroll offset inside the range the settings window can show.
func ClampScroll(n, limit int32) int32 {
	if n < 0 {
		return 0
	}
	if limit < 0 {
		limit = 0
	}
	if n > limit {
		return limit
	}
	return n
}
