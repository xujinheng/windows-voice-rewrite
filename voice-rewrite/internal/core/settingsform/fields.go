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
