package settingsform

import "testing"

func TestSettingsRailPaletteKeepsLightAndDarkDistinct(t *testing.T) {
	if RailWidth != 194 || RailDividerX != RailWidth-1 {
		t.Fatal("rail alignment")
	}
	if DirectFieldRadius == FallbackFieldRadius || NavRadius != FallbackFieldRadius {
		t.Fatal("radius")
	}
	if RailColor(true) == RailColor(false) {
		t.Fatal("settings sidebar lost its light/dark palette")
	}
}
