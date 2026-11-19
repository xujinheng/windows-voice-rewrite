package settingsform

const (
	RailWidth           int32 = 194
	RailDividerX        int32 = 193
	DirectFieldRadius   int32 = 8
	FallbackFieldRadius int32 = 9
	NavRadius           int32 = 9
	BrandTitle                = "VoiceRewrite"
	RailCategory              = "设置分类"
)

// RailColor is the settings navigation rail. It is not the window background.
func RailColor(dark bool) uint32 {
	if dark {
		return 0x181B23
	}
	return 0xF0F1F6
}

type Box struct{ X, Y, W, H int32 }

func RailFill(height int32) Box { return Box{0, 0, RailWidth, height} }

func RailDivider(height int32) Box { return Box{RailDividerX, 0, 1, height} }

func DirectBrand() Box { return Box{20, 23, 167, 27} }

func DirectCategory() Box { return Box{22, 75, 150, 20} }

func DirectRailRule() Box { return Box{20, 246, 154, 1} }

// DirectTitle returns the title origin, the amount subtracted from the live width, and the height.
// The subtraction stays in the host so a fractional logical width is not truncated first.
func DirectTitle() (x, y, cut, h int32) { return 220, 35, 245, 39 }

func DirectSubtitle() (x, y, cut, h int32) { return 222, 79, 244, 23 }

func DirectSectionRule() (x, y, cut, h int32) { return 219, 121, 239, 1 }

func DirectSection() Box { return Box{224, 132, 235, 26} }

func DirectFooterRule() (x, yCut, wCut, h int32) { return RailWidth, 77, RailWidth, 1 }

type Edges struct{ Left, Top, Right, Bottom int32 }

// FallbackTitle is the GDI title bounds. Its right edge is a client inset, not the Direct2D title width.
func FallbackTitle() Edges { return Edges{220, 34, 15, 80} }

// FallbackSubtitle is the GDI subtitle bounds. It is not the Direct2D subtitle box.
func FallbackSubtitle() Edges { return Edges{222, 80, 15, 110} }

func NavAccent(item Edges) Edges {
	return Edges{item.Left + 2, item.Top + 9, item.Left + 5, item.Bottom - 9}
}

func NavLabelLeft(left int32) int32 { return left + 18 }

func NavFocus(item Edges) Edges {
	return Edges{item.Left + 8, item.Top + 4, item.Right - 6, item.Bottom - 4}
}
