package ux

import (
	"errors"

	"voicerewrite/internal/core/indicator"
)

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
