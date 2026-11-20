package texttarget

import "testing"

func TestEditorClassification(t *testing.T) {
	if !NativeEditClass("Edit") || NativeEditClass("Chrome_RenderWidgetHostHWND") {
		t.Fatal("native edit class")
	}
}
