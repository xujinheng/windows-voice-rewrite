// Package input holds portable pointer-role decisions shared by desktop hosts.
package input

// SideButtonFlow maps a physical button to dictation, rewrite, or ignore.
// A zero configured button never matches, so an unbound role cannot steal input.
func SideButtonFlow(button, rewriteButton, dictationButton uint16) string {
	switch button {
	case dictationButton:
		if button != 0 {
			return "dictation"
		}
	case rewriteButton:
		if button != 0 {
			return "rewrite"
		}
	}
	return "ignore"
}
