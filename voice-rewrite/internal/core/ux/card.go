package ux

type CardButton int

const (
	CardCopy CardButton = iota
	CardApply
	CardClose
)

// CardButtonFace chooses the painted label and whether that button is the primary action.
// Copy is primary only when the card cannot apply. Apply stays primary. Close is the mark, not a primary action.
func CardButtonFace(button CardButton, allowApply bool) (label string, primary bool, ok bool) {
	switch button {
	case CardCopy:
		return CopyAction, !allowApply, true
	case CardApply:
		return ApplyAction, true, true
	case CardClose:
		return CloseGlyph, false, true
	default:
		return "", false, false
	}
}

type CardSurface int

const (
	CardChrome CardSurface = iota
	CardNote
	CardEditor
)

// CardInk reports the muted note and the editor surface. The title stays on the window background.
func CardInk(surface CardSurface) (muted, editor bool) {
	switch surface {
	case CardNote:
		return true, false
	case CardEditor:
		return false, true
	default:
		return false, false
	}
}
