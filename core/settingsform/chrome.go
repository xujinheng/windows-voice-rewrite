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
