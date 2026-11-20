package ux

import (
	"errors"
	"strings"

	"voicerewrite/internal/core/indicator"
)

const (
	PasteReceiptMinimumMilliseconds = 700

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
	return DictationTargetReady(keysReleased, windowOK, password)
}

// DictationTargetReady checks the same native delivery facts on both hosts.
// A result has already passed RouteDictationTranscript before this check.
func DictationTargetReady(keysReleased, windowOK, password func() bool) error {
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

// Delivery is how a finished voice result meets the user.
// A false SetPhase leaves the pill where the host already put it.
type Delivery struct {
	ShowCard    bool
	Title       string
	Phase       indicator.Phase
	SetPhase    bool
	TraySuccess bool
}

// DictationInserted is the pill and card after a dictation paste attempt.
func DictationInserted(err error) Delivery {
	// A non-nil error leaves delivery uncertain (the paste event may have fired).
	// Preserve the manual result instead of retrying or claiming success.
	if err != nil {
		return Delivery{ShowCard: true, Title: DictationCopyManually, Phase: indicator.Fallback, SetPhase: true}
	}
	// A nil error means the OS accepted SendInput, not that the editor wrote text.
	return Delivery{Phase: indicator.Success, SetPhase: true, TraySuccess: true}
}

// RewritePasted is the pill after that paste.
// A skipped or failed paste keeps the current pill and still shows the result card.
func RewritePasted(allowApply bool, err error) Delivery {
	if allowApply && err == nil {
		return Delivery{Phase: indicator.Success, SetPhase: true, TraySuccess: true}
	}
	return Delivery{ShowCard: true, TraySuccess: true}
}
