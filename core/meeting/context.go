package meeting

import (
	"strings"
	"unicode"
	"unicode/utf8"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
)

const (
	// Only recent, bounded completed turns may accompany a model request.
	MaxContextTurns      = 3
	MaxContextRunes      = 1800
	MaxContextTurnRunes  = 900
	MaxGlossaryLineRunes = 160
)

// PrepareQuery applies one policy to Windows and macOS before a Meeting model
// request. Captions and literal translation never inherit the conversation.
// Compose receives history only when the speaker refers to it. Glossary
// spellings are always eligible, but unrelated profile facts are not.
func PrepareQuery(cfg coreconfig.Config, q Query) Query {
	q.Background = clippedRunes(strings.TrimSpace(cfg.MeetingBackground), MaxBackgroundRunes)
	q.Terms = clippedRunes(strings.TrimSpace(cfg.MeetingTerms), MaxTermsRunes)
	switch q.Mode {
	case "caption":
		q.Prior = nil
	case "express":
		q.Prior, q.Background = nil, ""
		q.Terms = MatchingTerms(q.Terms, q.UserPoints)
	case "compose":
		spoken := q.UserPoints
		if UsesConversationContext(q.UserPoints) {
			q.Prior = RecentContext(q.Prior)
			spoken += "\n" + strings.Join(q.Prior, "\n")
		} else {
			q.Prior, q.Background = nil, ""
		}
		q.Terms = MatchingTerms(q.Terms, spoken)
	case "replies":
		q.Prior = RecentContext(q.Prior)
		// A glossary is a vocabulary hint, not instructions to steer every
		// answer toward every project named in the saved profile.
		q.Terms = MatchingTerms(q.Terms, q.Latest+"\n"+strings.Join(q.Prior, "\n"))
	case "refine":
		// Refinement edits the selected sentence. Old turns and background
		// must not silently introduce facts or commitments into that edit.
		q.Prior, q.Background = nil, ""
		q.Terms = MatchingTerms(q.Terms, q.SelectedReply+"\n"+q.UserPoints)
	default:
		q.Prior, q.Background, q.Terms = nil, "", ""
	}
	return q
}

// RecentContext prefers the end of the last completed turns over very old
// transcript history. Input is never modified, even when it is over budget.
func RecentContext(prior []string) []string {
	result := make([]string, 0, MaxContextTurns)
	budget := MaxContextRunes
	for i := len(prior) - 1; i >= 0 && len(result) < MaxContextTurns && budget > 0; i-- {
		line := strings.TrimSpace(prior[i])
		if line == "" {
			continue
		}
		limit := min(MaxContextTurnRunes, budget)
		runes := []rune(line)
		if len(runes) > limit {
			line = string(runes[len(runes)-limit:])
			runes = []rune(line)
		}
		budget -= len(runes)
		result = append(result, line)
	}
	for l, r := 0, len(result)-1; l < r; l, r = l+1, r-1 {
		result[l], result[r] = result[r], result[l]
	}
	return result
}

// MatchingTerms supplies only glossary lines actually mentioned by the
// speaker. Each side of a bilingual "Chinese = English" entry can match;
// shorter English acronyms need word boundaries to avoid false positives.
func MatchingTerms(terms, spoken string) string {
	if spoken == "" || terms == "" {
		return ""
	}
	var matched []string
	budget := MaxTermsRunes
	for _, raw := range strings.Split(terms, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || utf8.RuneCountInString(line) > MaxGlossaryLineRunes {
			continue
		}
		pieces := strings.FieldsFunc(line, func(r rune) bool {
			return r == '=' || r == '＝' || r == '：' || r == ':' || r == '|' || r == '｜' || r == '\t'
		})
		found := false
		for _, piece := range pieces {
			if glossaryMention(spoken, strings.TrimSpace(piece)) {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		n := utf8.RuneCountInString(line)
		if n > budget {
			break
		}
		matched = append(matched, line)
		budget -= n
	}
	return strings.Join(matched, "\n")
}

func glossaryMention(spoken, term string) bool {
	if term == "" {
		return false
	}
	haystack, needle := []rune(strings.ToLower(spoken)), []rune(strings.ToLower(term))
	for start := 0; start+len(needle) <= len(haystack); start++ {
		ok := true
		for i := range needle {
			if haystack[start+i] != needle[i] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if unicode.Is(unicode.Han, needle[0]) || unicode.Is(unicode.Han, needle[len(needle)-1]) {
			return true
		}
		if start > 0 && isWordRune(haystack[start-1]) {
			continue
		}
		end := start + len(needle)
		if end < len(haystack) && isWordRune(haystack[end]) {
			continue
		}
		return true
	}
	return false
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }

func clippedRunes(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return value
}
