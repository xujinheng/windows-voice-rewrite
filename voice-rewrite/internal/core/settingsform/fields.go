package settingsform

const RowStep int32 = 54

const (
	frameX      int32 = 345
	frameWidth  int32 = 447
	frameHeight int32 = 34
	firstY      int32 = 169
	insetX      int32 = 14
	insetY      int32 = 5
	editHeight  int32 = 24
)

type Rect struct{ X, Y, W, H int32 }

func FieldFrame(row int) Rect {
	return Rect{frameX, firstY + int32(row)*RowStep, frameWidth, frameHeight}
}

func FieldEdit(row int) Rect {
	frame := FieldFrame(row)
	return Rect{frame.X + insetX, frame.Y + insetY, frame.W - 2*insetX, editHeight}
}

// ModelEdit leaves room for the native model-discovery action inside the shared field frame.
func ModelEdit(row int) Rect {
	field := FieldEdit(row)
	field.W -= 110
	return field
}

func ModelDiscoverButton(row int) Rect {
	frame := FieldFrame(row)
	return Rect{frame.X + frame.W - 96, frame.Y + 2, 92, 30}
}

// ModelDiscoveryMayPresent rejects an old response, an abandoned settings page,
// and a cancelled request even if the previous window handle is reused.
func ModelDiscoveryMayPresent(requestID, currentID uint64, requestPage, visiblePage int) bool {
	return requestID != 0 && requestID == currentID && requestPage == visiblePage
}

const MaxModelMenuChoices = 40

func FieldLabel(row int) Rect {
	frame := FieldFrame(row)
	return Rect{224, frame.Y + 5, 119, 24}
}

// PaintedFrames is the rounded outline behind each stacked settings field.
// The input page paints two shortcut frames. The other pages paint three text fields.
func PaintedFrames(page string) []Rect {
	rows := 3
	shortcut := page == "input"
	if shortcut {
		rows = 2
	}
	frames := make([]Rect, rows)
	for i := range frames {
		if shortcut {
			frame := ShortcutFrame()
			frame.Y += int32(i) * RowStep
			frames[i] = frame
			continue
		}
		frames[i] = FieldFrame(i)
	}
	return frames
}

func ShortcutFrame() Rect {
	frame := FieldFrame(0)
	frame.W = 226
	return frame
}

func ShortcutEdit() Rect {
	frame := ShortcutFrame()
	return Rect{frame.X + insetX, frame.Y + insetY, frame.W - 2*insetX, editHeight}
}

func ProbeStatusBox() Rect { return Rect{352, 324, 435, 48} }

func ProbeButtonBox() Rect { return Rect{696, 130, 92, 30} }

func WindowsInputNote() Rect { return Rect{226, 345, 565, 44} }

func FooterButton(anchor Anchor) Rect {
	switch anchor {
	case AnchorSave:
		return Rect{220, 418, 124, 34}
	case AnchorAdvanced:
		return Rect{358, 418, 112, 34}
	case AnchorClose:
		return Rect{710, 418, 94, 34}
	default:
		return Rect{}
	}
}

func SettingsHint() Rect { return Rect{223, 457, 560, 22} }

func NavButton(index int) Rect {
	return Rect{12, 110 + int32(index)*41, 170, 35}
}

func CaptureButton(row int) Rect {
	frame := FieldFrame(row)
	return Rect{593, frame.Y, 194, 34}
}

func MicrophoneList(row int) Rect {
	frame := FieldFrame(row)
	return Rect{frame.X + 5, frame.Y + 4, frame.W - 10, 190}
}
