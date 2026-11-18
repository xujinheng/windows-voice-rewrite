package ux

import "strings"

// DictationRoute is the single Go-owned ASR-result decision used by both hosts.
// A native host supplies only whether it has a captured insertion target;
// the native editor remains responsible for validating that target at write time.
type DictationRoute string

const (
	DictationRouteEmpty   DictationRoute = "empty"
	DictationRouteInsert  DictationRoute = "insert"
	DictationRoutePreview DictationRoute = "preview"
)

// RouteDictationTranscript must not authorize an insertion of empty ASR output.
// No-target results are preserved for copying instead of discarded.
func RouteDictationTranscript(text string, hasTarget bool) DictationRoute {
	if strings.TrimSpace(text) == "" {
		return DictationRouteEmpty
	}
	if !hasTarget {
		return DictationRoutePreview
	}
	return DictationRouteInsert
}
