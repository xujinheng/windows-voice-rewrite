// Package texttarget classifies Win32 window classes used when writing text.
// A browser password widget is not identified from an edit style. When the
// accessibility probe cannot run, the whole browser or Electron window is
// treated as sensitive.
package texttarget

import "strings"

const passwordStyle uint32 = 0x0020

// NativeEditClass reports a Win32 edit, rich edit, or WinForms edit.
func NativeEditClass(className string) bool {
	className = strings.ToLower(className)
	return className == "edit" || strings.HasPrefix(className, "richedit") || strings.HasPrefix(className, "windowsforms10.edit")
}

// NativePassword is true only for a native edit whose style includes ES_PASSWORD.
func NativePassword(className string, style uint32) bool {
	return NativeEditClass(className) && style&passwordStyle != 0
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

// BrowserWindow reports a Chrome, Edge, Firefox, CEF, or Electron top-level
// window. It is the fail-closed class check used when a password probe cannot
// inspect the focused control.
func BrowserWindow(className string) bool {
	className = strings.ToLower(className)
	return strings.Contains(className, "chrome_widgetwin") ||
		strings.Contains(className, "mozillawindowclass") ||
		strings.Contains(className, "cefclientwindow") ||
		strings.Contains(className, "electron")
}
