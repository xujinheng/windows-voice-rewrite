// Package desktop keeps a window inside a monitor work area, including
// monitors whose coordinates are negative.
package desktop

type WorkArea struct{ Left, Top, Right, Bottom int32 }

func Fit(w, h int32, work WorkArea, margin int32) (int32, int32) {
	availableW, availableH := work.Right-work.Left-2*margin, work.Bottom-work.Top-2*margin
	if availableW < 1 {
		availableW = 1
	}
	if availableH < 1 {
		availableH = 1
	}
	if w > availableW {
		w = availableW
	}
	if h > availableH {
		h = availableH
	}
	return w, h
}

// Center places a window in the middle of the work area. A window larger
// than that area stays at the top-left; it is not pulled in from the right.
func Center(w, h int32, work WorkArea) (int32, int32) {
	x := work.Left + (work.Right-work.Left-w)/2
	y := work.Top + (work.Bottom-work.Top-h)/2
	if x < work.Left {
		x = work.Left
	}
	if y < work.Top {
		y = work.Top
	}
	return x, y
}

func Clamp(x, y, w, h int32, work WorkArea) (int32, int32) {
	maxX, maxY := work.Right-w, work.Bottom-h
	if maxX < work.Left {
		maxX = work.Left
	}
	if maxY < work.Top {
		maxY = work.Top
	}
	if x < work.Left {
		x = work.Left
	}
	if x > maxX {
		x = maxX
	}
	if y < work.Top {
		y = work.Top
	}
	if y > maxY {
		y = maxY
	}
	return x, y
}

// PlacePopup sits a result card just below and to the right of the pointer,
// then keeps the whole card inside the work area.
func PlacePopup(anchorX, anchorY, w, h int32, work WorkArea) (int32, int32, int32, int32) {
	w, h = Fit(w, h, work, 12)
	x, y := Clamp(anchorX+18, anchorY+22, w, h, work)
	return x, y, w, h
}
