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

// ApplyIgnored reports that a manual apply must return before taking the busy flag.
// A missing selection, a preview-only card, a canceled card, or a stale generation
// does not show the busy note.
func ApplyIgnored(hasSelection, allowApply, canceled, generationValid bool) bool {
	return !hasSelection || !allowApply || canceled || !generationValid
}

// ApplyContinuation is the manual apply after ApplyIgnored returned false.
type ApplyContinuation int

const (
	// ApplyBusyNote shows the busy sentence and leaves the flag with the task that owns it.
	ApplyBusyNote ApplyContinuation = iota
	// ApplyEditorClosed reports the closed editor and releases the flag taken for this apply.
	ApplyEditorClosed
	// ApplyReplace focuses the editor and replaces the selection.
	ApplyReplace
)

// ContinueApply chooses that continuation.
// A busy flag that was not taken stays with the other task even when the editor is closed.
func ContinueApply(busyTaken, editorOpen bool) ApplyContinuation {
	if !busyTaken {
		return ApplyBusyNote
	}
	if !editorOpen {
		return ApplyEditorClosed
	}
	return ApplyReplace
}

// ResultWritten refuses a control that stayed empty after the text was set.
func ResultWritten(length int) error {
	if length == 0 {
		return errors.New(UnwrittenCard)
	}
	return nil
}
