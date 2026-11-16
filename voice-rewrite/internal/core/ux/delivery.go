package ux

import (
	"errors"
	"fmt"
	"strings"

	"voicerewrite/internal/core/indicator"
)

const (
	// DictationReleaseKeys is the Windows wait for a held shortcut. Callers keep their own timeouts.
	DictationReleaseKeys = "请松开快捷键后重试"
	// DictationWindowChanged is the Windows dictation paste cancelled because the target window moved.
	DictationWindowChanged = "写入前窗口已变化，已取消听写"
	// DictationRestoreFailed is the dictation clipboard restore failure. The replace path keeps its own sentence.
	DictationRestoreFailed = "已触发粘贴，但剪贴板恢复失败"
)

// DictationInsertReady is the order of checks before a dictation paste changes the clipboard.
// Later checks are not called after an earlier one fails.
func DictationInsertReady(text string, keysReleased, windowOK, password func() bool) error {
	if strings.TrimSpace(text) == "" {
		return errors.New(DictationEmpty)
	}
	if !keysReleased() {
		return errors.New(DictationReleaseKeys)
	}
	if !windowOK() {
		return errors.New(DictationWindowChanged)
	}
	if password() {
		return errors.New(PasswordDictation)
	}
	return nil
}

// DictationWindowStillOpen repeats the window check after the clipboard already holds the transcript.
func DictationWindowStillOpen(windowOK func() bool) error {
	if !windowOK() {
		return errors.New(DictationWindowChanged)
	}
	return nil
}

// DictationClipboardRestored wraps a restore error. A nil error stays nil.
func DictationClipboardRestored(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", DictationRestoreFailed, err)
}

// DictationCaptureProblem prefers a capture error over the short-buffer sentence.
func DictationCaptureProblem(err error, tooShort bool) error {
	if err != nil {
		return err
	}
	if tooShort {
		return errors.New(DictationTooShort)
	}
	return nil
}

// RewriteCaptureProblem prefers a capture error over the short-buffer sentence.
// The caller does not change the pill for this error.
func RewriteCaptureProblem(err error, tooShort bool) error {
	if err != nil {
		return err
	}
	if tooShort {
		return errors.New(RewriteTooShort)
	}
	return nil
}

// Delivery is how a finished voice result meets the user.
// A false SetPhase leaves the pill where the host already put it.
type Delivery struct {
	ShowCard    bool
	Title       string
	Phase       indicator.Phase
	SetPhase    bool
	TraySuccess bool
}

// DictationUnfocused is the card used when the target window changed during recognition.
func DictationUnfocused() Delivery {
	return Delivery{ShowCard: true, Title: DictationNotInserted, Phase: indicator.Fallback, SetPhase: true}
}

// DictationInserted is the pill and card after a dictation paste attempt.
func DictationInserted(err error) Delivery {
	if err != nil {
		return Delivery{ShowCard: true, Title: DictationCopyManually, Phase: indicator.Fallback, SetPhase: true}
	}
	return Delivery{Phase: indicator.Success, SetPhase: true, TraySuccess: true}
}

// RewriteApplying is the pill shown while a spoken rewrite is pasted.
func RewriteApplying() Delivery {
	return Delivery{Phase: indicator.Applying, SetPhase: true}
}

// RewritePasted is the pill after that paste.
// A skipped or failed paste keeps the current pill and still shows the result card.
func RewritePasted(allowApply bool, err error) Delivery {
	if allowApply && err == nil {
		return Delivery{Phase: indicator.Success, SetPhase: true, TraySuccess: true}
	}
	return Delivery{ShowCard: true, TraySuccess: true}
}
