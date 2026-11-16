package popup

import "testing"

func TestEmptyCardStaysDifferentFromAModelFailure(t *testing.T) {
	err := ResultShown(" \r\n ")
	if err == nil || err.Error() != EmptyCard || err.Error() == "文本模型返回空内容；请换用非 thinking 模式的 Instruct 文本模型" {
		t.Fatal(err)
	}
	if ResultShown("a") != nil {
		t.Fatal("text")
	}
	if got := EditorText("a\r\nb\nc"); got != "a\r\nb\r\nc" {
		t.Fatal(got)
	}
	if !ApplyIgnored(false, true, false, true) || !ApplyIgnored(true, false, false, true) || !ApplyIgnored(true, true, true, true) || !ApplyIgnored(true, true, false, false) {
		t.Fatal("ignored")
	}
	if ApplyIgnored(true, true, false, true) {
		t.Fatal("ready")
	}
	if ContinueApply(false, true) != ApplyBusyNote || ContinueApply(false, false) != ApplyBusyNote {
		t.Fatal("busy apply keeps the flag")
	}
	if ContinueApply(true, false) != ApplyEditorClosed || ContinueApply(true, true) != ApplyReplace {
		t.Fatal("editor")
	}
	if ResultWritten(0) == nil || ResultWritten(0).Error() != UnwrittenCard || ResultWritten(3) != nil {
		t.Fatal("written")
	}
}
