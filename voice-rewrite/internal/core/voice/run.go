// Package voice owns the portable one-shot dictation and selected-text pipeline.
// Native hosts still own audio capture, focus, paste and feedback.
package voice

import (
	"context"
	"errors"
	"strings"

	"voicerewrite/internal/core/audio"
	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/speech"
	"voicerewrite/internal/core/textedit"
	"voicerewrite/internal/core/ux"
)

type Result struct {
	Text        string `json:"text"`
	Intent      string `json:"intent,omitempty"`
	Instruction string `json:"instruction,omitempty"`
	Route       string `json:"route"`
}

// PCMError marks audio admission failures, distinct from provider failures.
// The native hosts retain their own error presentation and OS recovery actions.
type PCMError struct{ message string }

func (e *PCMError) Error() string { return e.message }

// Run is the shared production task path for Windows workers and macOS C ABI.
// A result route states the desired action; native focus/selection checks can
// still prevent delivery, and a paste event is never proof of insertion.
func Run(ctx context.Context, cfg coreconfig.Config, pcm []byte, selected string, rewrite bool, onTranscribed func(string)) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	// Both production hosts supply normalized config values.
	maxBytes := min(cfg.MaxRecordSeconds, coreconfig.RecordSecondsMax) * audio.PCMBytesPerSecond
	if len(pcm) == 0 || len(pcm)%2 != 0 || len(pcm) > maxBytes {
		return Result{}, &PCMError{"PCM must be 16 kHz mono PCM16, at least 0.1s and within duration limit"}
	}
	if audio.BelowMinCapture(pcm) {
		if rewrite {
			return Result{}, &PCMError{ux.RewriteTooShort}
		}
		return Result{}, &PCMError{ux.DictationTooShort}
	}
	if rewrite {
		if strings.TrimSpace(selected) == "" {
			return Result{}, errors.New(ux.EmptySelection)
		}
		if err := textedit.SelectedLengthError(cfg.MaxSelectedRunes, selected); err != nil {
			return Result{}, err
		}
	}
	instruction, err := speech.Transcribe(ctx, cfg, pcm)
	if err != nil {
		return Result{}, err
	}
	if !rewrite {
		return Result{Text: instruction, Route: "insert"}, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if onTranscribed != nil {
		onTranscribed(instruction)
	}
	answer, err := textedit.ResolveSelectedTask(ctx, cfg, selected, instruction)
	if err != nil {
		return Result{}, err
	}
	route := "preview"
	if textedit.RouteTask(answer, instruction) == textedit.Replace {
		route = "replace"
	}
	return Result{Text: answer.Result, Intent: answer.Intent, Instruction: instruction, Route: route}, nil
}
