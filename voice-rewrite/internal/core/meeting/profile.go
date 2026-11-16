package meeting

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// MaxBackgroundRunes and MaxTermsRunes are counted before trimming.
	// The Windows profile fields use the same numbers as their text limits.
	MaxBackgroundRunes = 4000
	MaxTermsRunes      = 2000
	// MaxRefineInstructionRunes is the accepted length after trim.
	// The Windows field stops typing at 600; this function still allows 2000.
	MaxRefineInstructionRunes = 2000
)

// AcceptProfile rejects over-long meeting notes, then trims what will be stored.
func AcceptProfile(background, terms string) (string, string, error) {
	if utf8.RuneCountInString(background) > MaxBackgroundRunes || utf8.RuneCountInString(terms) > MaxTermsRunes {
		return "", "", fmt.Errorf("背景最多 %d 字，术语最多 %d 字", MaxBackgroundRunes, MaxTermsRunes)
	}
	return strings.TrimSpace(background), strings.TrimSpace(terms), nil
}

// AcceptRefine reports whether a suggestion index and instruction may be sent.
// An empty or too-long instruction is ignored, matching the previous host gate.
func AcceptRefine(index int, instruction string) (string, bool) {
	instruction = strings.TrimSpace(instruction)
	if index < 0 || index >= len(ReplyBatch{}.Lines) || instruction == "" || utf8.RuneCountInString(instruction) > MaxRefineInstructionRunes {
		return "", false
	}
	return instruction, true
}
