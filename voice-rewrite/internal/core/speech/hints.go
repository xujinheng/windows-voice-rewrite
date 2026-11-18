package speech

import (
	"strings"
	"unicode"
	"unicode/utf8"

	coreconfig "voicerewrite/internal/core/config"
)

// Static spoken-form candidates are vocabulary, NEVER transcript substitutions.
// An ASR prompt is only useful if the selected inference runtime consumes it.
// No user selection, Meeting profile, system prompt or transcript is included.
var engineeringVocabulary = []string{
	"benchmark", "prompt", "Go", "Swift", "macOS", "Windows", "PR",
	"CI", "rebase", "merge", "dependency", "issue", "latency",
	"regression", "OpenAI", "Qwen3-ASR", "llama.cpp", "VoiceRewrite",
	"P50", "P95", "GitHub Actions", "Go Core",
}

const maxHintBytes = 1024
const maxHintTerms = 32

// auto-supported is conservative: model name alone is NOT evidence of
// support on an arbitrary OpenAI-compatible provider or llama.cpp server.
// vocabulary-only explicitly opts into the multipart prompt for a user-verified
// endpoint. It does not retry on 400/422, so no hidden double billing.
func hintEnabled(cfg coreconfig.Config) bool {
	switch cfg.ASRHintMode {
	case coreconfig.ASRHintVocabulary:
		return true
	case coreconfig.ASRHintAuto:
		base := strings.TrimRight(strings.ToLower(cfg.ASRBaseURL), "/")
		if base != "https://api.openai.com/v1" {
			return false
		}
		return cfg.ASRModel == "gpt-transcribe" || cfg.ASRModel == "whisper-1" || cfg.ASRModel == "gpt-4o-transcribe" || cfg.ASRModel == "gpt-4o-mini-transcribe"
	default:
		return false
	}
}

// safeTerm accepts only short vocabulary fragments, never arbitrary instructions.
// Optional aliases use "canonical = spoken alias" on individual lines.
// Chinese aliases and spelled-out initials are permitted; no output rewriting.
func safeTerm(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\t<>:{}[]\\|\"'") {
		return "", false
	}
	parts := strings.Split(value, "=")
	if len(parts) > 2 {
		return "", false
	}
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || len(part) > 42 || len(strings.Fields(part)) > 3 {
			return "", false
		}
		for _, r := range part {
			if !(unicode.IsLetter(r) || unicode.IsNumber(r) || r == ' ' ||
				r == '.' || r == '-' || r == '_' || r == '+' || r == '/' || r == '#') {
				return "", false
			}
		}
		if i == 0 && !strings.ContainsFunc(part, func(r rune) bool { return unicode.IsLetter(r) }) {
			return "", false
		}
	}
	for _, word := range strings.Fields(strings.ToLower(value)) {
		switch word {
		case "ignore", "override", "instruction", "instructions", "system", "developer", "obey", "forget", "execute", "assistant":
			return "", false
		}
	}
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]) + " (" + strings.TrimSpace(parts[1]) + ")", true
	}
	return value, true
}

// DictationHint is shared by Windows, voicebridge and selected spoken tasks.
// No network probe, no transcription mutation, no additional HTTP request.
func DictationHint(cfg coreconfig.Config) string {
	if !hintEnabled(cfg) {
		return ""
	}
	const prefix = "Vocabulary: "
	var terms []string
	seen := map[string]bool{}
	add := func(raw string) {
		if len(terms) >= maxHintTerms {
			return
		}
		canonical, ok := safeTerm(raw)
		if !ok {
			return
		}
		key := strings.ToLower(strings.TrimSpace(strings.SplitN(raw, "=", 2)[0]))
		if seen[key] {
			return
		}
		n := len(prefix) + len(strings.Join(terms, ", ")) + len(canonical)
		if len(terms) > 0 {
			n += 2
		}
		if n > maxHintBytes {
			return
		}
		seen[key] = true
		terms = append(terms, canonical)
	}
	// User terms have priority, but are never treated as model instructions.
	for _, term := range strings.FieldsFunc(cfg.ASRVocabulary, func(r rune) bool { return r == '\n' || r == ',' || r == ';' }) {
		add(term)
	}
	for _, term := range engineeringVocabulary {
		add(term)
	}
	if len(terms) == 0 {
		return ""
	}
	return prefix + strings.Join(terms, ", ")
}
