// Package texttarget classifies Win32 editor window types.
package texttarget

import "strings"

// NativeEditClass reports a Win32 edit, rich edit, or WinForms edit.
func NativeEditClass(className string) bool {
	className = strings.ToLower(className)
	return className == "edit" || strings.HasPrefix(className, "richedit") || strings.HasPrefix(className, "windowsforms10.edit")
}
