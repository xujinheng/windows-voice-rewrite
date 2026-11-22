// Package asrtext owns Qwen3-ASR transcript marker normalization for
// VoiceRewrite's shared ASR client and local model server.
package asrtext

import (
	"regexp"
	"strings"
)

// Only a language header directly followed by <asr_text> is metadata.
var languageMarker = regexp.MustCompile(`(?i)language[ \t]+[a-z][a-z0-9 ,_+\-/]{0,79}[ \t]*<asr_text>`)
var leadingMarker = regexp.MustCompile(`(?i)^\s*<asr_text>\s*`)

// StripMetadata removes Qwen3-ASR language headers: remove language
// headers, leaving other recognized tokens to the consuming client.
func StripMetadata(s string) string {
	return strings.TrimSpace(languageMarker.ReplaceAllString(s, ""))
}

// Clean strips additional ASR framing for the consuming VoiceRewrite app.
func Clean(s string) string {
	s = leadingMarker.ReplaceAllString(StripMetadata(s), "")
	s = strings.ReplaceAll(s, "<|endoftext|>", "")
	return strings.TrimSpace(s)
}
