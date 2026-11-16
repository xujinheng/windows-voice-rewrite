package popup

import (
	"errors"
	"strings"
)

const (
	// EmptyCard is the Windows result card with no visible text.
	// It is not the model-response sentence used when a completion is empty.
	EmptyCard     = "AI 未返回可显示的文字"
	UnwrittenCard = "结果正文未写入文本控件"
)

// ResultShown refuses a card whose body is empty or only whitespace.
func ResultShown(body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New(EmptyCard)
	}
	return nil
}

// EditorText uses Windows newlines in the native edit control.
func EditorText(body string) string {
	return strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
}

// ResultWritten refuses a control that stayed empty after the text was set.
func ResultWritten(length int) error {
	if length == 0 {
		return errors.New(UnwrittenCard)
	}
	return nil
}
