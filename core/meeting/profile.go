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
	// RefineFieldLimit bounds the input UI and the shared request protocol.
	// A distinct 2000-rune limit applies to free-form compose and express.
	RefineFieldLimit = 600
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
	if index < 0 || index >= len(ReplyBatch{}.Lines) || instruction == "" || utf8.RuneCountInString(instruction) > RefineFieldLimit {
		return "", false
	}
	return instruction, true
}
