package texttarget

import "testing"

func TestEditorClassification(t *testing.T) {
	if !NativeEditClass("Edit") || NativeEditClass("Chrome_RenderWidgetHostHWND") {
		t.Fatal("native edit class")
	}
	if !BrowserEditorSurface("Chrome_WidgetWin_1", "Chrome_RenderWidgetHostHWND") {
		t.Fatal("chrome surface")
	}
	if BrowserEditorSurface("Notepad", "Edit") || BrowserEditorSurface("Chrome_WidgetWin_1", "") {
		t.Fatal("non-browser surface")
	}
}
