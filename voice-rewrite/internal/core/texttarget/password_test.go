package texttarget

import "testing"

func TestNativePassword(t *testing.T) {
	for _, tc := range []struct {
		class string
		style uint32
		want  bool
	}{
		{"Edit", 0x0020, true},
		{"RichEdit50W", 0x0020, true},
		{"WindowsForms10.EDIT.app.0", 0x0020, true},
		{"Edit", 0, false},
		{"Chrome_RenderWidgetHostHWND", 0x0020, false},
		{"Static", 0x0020, false},
	} {
		if got := NativePassword(tc.class, tc.style); got != tc.want {
			t.Errorf("NativePassword(%q,%#x)=%t want %t", tc.class, tc.style, got, tc.want)
		}
	}
	if !NativeEditClass("Edit") || NativeEditClass("Chrome_RenderWidgetHostHWND") {
		t.Fatal("native edit class")
	}
	if !BrowserEditorSurface("Chrome_WidgetWin_1", "Chrome_RenderWidgetHostHWND") {
		t.Fatal("chrome surface")
	}
	if BrowserEditorSurface("Notepad", "Edit") || BrowserEditorSurface("Chrome_WidgetWin_1", "") {
		t.Fatal("non-browser surface")
	}
	for _, tc := range []struct {
		class string
		want  bool
	}{
		{"Chrome_WidgetWin_1", true},
		{"MozillaWindowClass", true},
		{"CEFCLIENTWINDOW", true},
		{"Electron_Window", true},
		{"Notepad", false},
		{"Edit", false},
	} {
		if got := BrowserWindow(tc.class); got != tc.want {
			t.Errorf("BrowserWindow(%q)=%t want %t", tc.class, got, tc.want)
		}
	}
}
