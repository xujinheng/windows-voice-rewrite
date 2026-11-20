// Package voice owns the portable one-shot dictation and selected-text pipeline.
// Native hosts still own audio capture, focus, paste and feedback.
package voice

import (
	"context"

	"voicerewrite/internal/core/audio"
	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/speech"
	"voicerewrite/internal/core/textedit"
	"voicerewrite/internal/core/ux"
)

type Result struct {
	Text        string
	Intent      string
	Instruction string
	Route       string
}

// PCMError marks audio admission failures, distinct from provider failures.
// The native hosts retain their own error presentation and OS recovery actions.
type PCMError struct{ message string }

func (e *PCMError) Error() string { return e.message }

// Run is the shared production task path for Windows workers and macOS C ABI.
// A result route states the desired action; native focus/selection checks can
// still prevent delivery, and a paste event is never proof of insertion.
func Run(ctx context.Context, cfg coreconfig.Config, pcm []byte, selected string, rewrite, canInsert bool, onTranscribed func(string)) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	maxBytes := cfg.MaxRecordSeconds * audio.PCMBytesPerSecond
	if maxBytes <= 0 || maxBytes > coreconfig.RecordSecondsMax*audio.PCMBytesPerSecond {
		maxBytes = coreconfig.RecordSecondsMax * audio.PCMBytesPerSecond
	}
	if len(pcm) == 0 || len(pcm)%2 != 0 || len(pcm) > maxBytes {
		return Result{}, &PCMError{"PCM must be 16 kHz mono PCM16, at least 0.1s and within duration limit"}
	}
	if audio.BelowMinCapture(pcm) {
		if rewrite {
			return Result{}, &PCMError{ux.RewriteTooShort}
		}
		return Result{}, &PCMError{ux.DictationTooShort}
	}
	if !rewrite {
		text, err := speech.Transcribe(ctx, cfg, pcm)
		if err != nil {
			return Result{}, err
		}
		route := "preview"
		if canInsert {
			route = "insert"
		}
		return Result{Text: text, Route: route}, nil
	}
	answer, instruction, err := textedit.ResolveSpokenTask(ctx, cfg, selected, pcm, onTranscribed)
	if err != nil {
		return Result{}, err
	}
	route := "preview"
	if textedit.RouteTask(answer, instruction) == textedit.Replace {
		route = "replace"
	}
	return Result{Text: answer.Result, Intent: answer.Intent, Instruction: instruction, Route: route}, nil
}
