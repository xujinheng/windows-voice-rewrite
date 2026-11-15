package main

import (
	"strings"

	coreconfig "voicerewrite/internal/core/config"
)

// Config is the portable application configuration model.
type Config = coreconfig.Config

func defaults() Config { return coreconfig.Defaults() }

// mouseSideButton reports the Windows XBUTTON identifier. Mouse4 is XBUTTON1
// and Mouse5 is XBUTTON2, independent of how the mouse is physically arranged.
func mouseSideButton(s string) (uint16, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mouse4", "xbutton1":
		return 1, true
	case "mouse5", "xbutton2":
		return 2, true
	default:
		return 0, false
	}
}
