package popup

import "testing"

func TestEditorTextNormalizesNewlines(t *testing.T) {
	if got := EditorText("a\r\nb\nc"); got != "a\r\nb\r\nc" {
		t.Fatal(got)
	}
}
