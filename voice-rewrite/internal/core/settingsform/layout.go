// Package settingsform is the logical-pixel geometry of the settings window.
// Hosts scale it to the monitor DPI when creating native controls.
package settingsform

import "voicerewrite/internal/contract"

const (
	DesignWidth   int32 = 816
	DesignHeight  int32 = 484
	WindowWidth   int32 = 860
	WindowHeight  int32 = 530
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

type Anchor int

const (
	AnchorNone Anchor = iota
	AnchorSave
	AnchorAdvanced
	AnchorClose
	AnchorHint
)

// AnchoredX pins the footer and hint when the settings window is scrolled.
// A view narrower than 650 logical pixels uses the compact row.
func AnchoredX(anchor Anchor, viewW, width, x int32) (int32, int32) {
	narrow := viewW < 650
	switch anchor {
	case AnchorSave:
		if narrow {
			return 12, width
		}
		return 220, width
	case AnchorAdvanced:
		if narrow {
			return 149, width
		}
		return 358, width
	case AnchorClose:
		if narrow {
			return 276, width
		}
		return viewW - width - 18, width
	case AnchorHint:
		x = 223
		if narrow {
			x = 14
		}
		if width > viewW-x-12 {
			width = viewW - x - 12
		}
		return x, width
	default:
		return x, width
	}
}

// PageSwitch is a settings navigation change.
// An out-of-range page is ignored. Leaving the current page stops shortcut capture.
// Staying on the page leaves capture alone. Either applied change clears the field focus.
type PageSwitch struct {
	Apply       bool
	StopCapture bool
	ClearFocus  bool
}

func SettingsPageSwitch(current, next, pageCount int) PageSwitch {
	if next < 0 || next >= pageCount {
		return PageSwitch{}
	}
	return PageSwitch{Apply: true, StopCapture: next != current, ClearFocus: true}
}

// ScrollStep is one scrollbar request in logical pixels.
// edge is -1 at the top, 1 at the bottom, and 0 when delta applies.
func ScrollStep(request uint16, view int32) (delta int32, edge int) {
	switch request {
	case 0:
		return -24, 0
	case 1:
		return 24, 0
	case 2:
		return -view + 48, 0
	case 3:
		return view - 48, 0
	case 6:
		return 0, -1
	case 7:
		return 0, 1
	default:
		return 0, 0
	}
}

func WheelScroll(current, delta int32) int32 {
	return current - delta/120*72
}

// FocusRow is the settings field outline for a focused edit.
// A negative input index means the focused control is not a text field.
func FocusRow(inputIndex int, rewrite, dictation bool) int {
	if rewrite {
		return 0
	}
	if dictation {
		return 1
	}
	if inputIndex < 0 {
		return -1
	}
	return inputIndex % 3
}

// ScrollLimit is how far the settings form can scroll for this view size.
func ScrollLimit(content, view int32) int32 {
	if view >= content {
		return 0
	}
	return content - view
}

// Place is one settings control after scroll. Navigation stays at its design
// position. The footer and hint are pinned. Every other control scrolls.
func Place(spec Spec, id int, hint bool, viewW, scrollX, scrollY int32) (x, y, w, h int32) {
	contentW := viewW
	if contentW < DesignWidth {
		contentW = DesignWidth
	}
	x, y, w, h = Layout(spec, contentW)
	x -= scrollX
	anchor := AnchorNone
	switch id {
	case contract.SaveButton:
		anchor = AnchorSave
	case contract.AdvancedButton:
		anchor = AnchorAdvanced
	case contract.CloseButton:
		anchor = AnchorClose
	case contract.NavSpeech, contract.NavAI, contract.NavInput:
		x = spec.X
	}
	if hint {
		anchor = AnchorHint
	}
	if anchor != AnchorNone {
		x, w = AnchoredX(anchor, viewW, w, x)
	}
	if id == contract.NavSpeech || id == contract.NavAI || id == contract.NavInput {
		return x, y, w, h
	}
	return x, y - scrollY, w, h
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
