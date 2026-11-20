// Package texttarget classifies Win32 editor window types.
package texttarget

import "strings"

// NativeEditClass reports a Win32 edit, rich edit, or WinForms edit.
func NativeEditClass(className string) bool {
	className = strings.ToLower(className)
	return className == "edit" || strings.HasPrefix(className, "richedit") || strings.HasPrefix(className, "windowsforms10.edit")
}

// BrowserEditorSurface is a Chrome, Edge, Electron, or Firefox renderer.
// A positive result only authorizes a guarded paste probe, not a direct write.
func BrowserEditorSurface(rootClass, focusClass string) bool {
	rootClass = strings.ToLower(rootClass)
	focusClass = strings.ToLower(focusClass)
	if focusClass == "" {
		return false
	}
	browserRoot := strings.Contains(rootClass, "chrome_widgetwin") || strings.Contains(rootClass, "mozillawindowclass")
	browserFocus := strings.Contains(focusClass, "chrome") || strings.Contains(focusClass, "mozilla") || strings.Contains(focusClass, "renderwidget")
	return browserRoot && browserFocus
}
