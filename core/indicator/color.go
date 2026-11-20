package indicator

// RGB is a pill color without alpha. Hosts add opacity when they paint.
type RGB struct {
	R, G, B uint8
}

// Opaque is 0xAARRGGBB with alpha 255, the form GDI+ solid fills use.
func (c RGB) Opaque() uint32 {
	return 0xFF000000 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

var (
	Pill            = RGB{0x20, 0x24, 0x2E}
	ClockText       = RGB{0xF5, 0xF3, 0xF7}
	LabelText       = RGB{0xEC, 0xE8, 0xF0}
	DictationAccent = RGB{0xE9, 0x81, 0xAA}
	RewriteAccent   = RGB{0xAF, 0xA5, 0xFF}
	ErrorAccent     = RGB{0xFF, 0x81, 0x8A}
	SuccessMark     = RGB{0x8D, 0xBD, 0xE8}
	AnswerMark      = RGB{0x78, 0xD9, 0xAD}
	FallbackMark    = RGB{0xF5, 0xC5, 0x6B}
	AttentionGlyph  = RGB{0x38, 0x19, 0x1D}
	CheckGlyph      = RGB{0x19, 0x34, 0x2A}
	CloseGlyph      = RGB{0xAA, 0xA5, 0xB3}
	CloseHover      = RGB{0x34, 0x3A, 0x49}
)

// Accent is the live recording and progress color. An error replaces the mode color.
func Accent(phase Phase, mode string) RGB {
	if phase == Error {
		return ErrorAccent
	}
	if mode == "改写" {
		return RewriteAccent
	}
	return DictationAccent
}

// TerminalMark is the circle drawn after recording stops.
func TerminalMark(phase Phase) RGB {
	switch phase {
	case Answer:
		return AnswerMark
	case Error:
		return ErrorAccent
	case Fallback:
		return FallbackMark
	default:
		return SuccessMark
	}
}
