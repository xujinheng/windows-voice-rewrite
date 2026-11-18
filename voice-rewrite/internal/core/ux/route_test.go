package ux

import "testing"

func TestRouteDictationTranscriptIsSharedAcrossHosts(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		target bool
		want   DictationRoute
	}{
		{"empty-with-target", "", true, DictationRouteEmpty},
		{"whitespace-with-target", " \t\n", true, DictationRouteEmpty},
		{"empty-without-target", "", false, DictationRouteEmpty},
		{"insert", "你好", true, DictationRouteInsert},
		{"fallback", "你好", false, DictationRoutePreview},
		{"preserves-unicode", "👩‍💻 résumé", true, DictationRouteInsert},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RouteDictationTranscript(tt.text, tt.target); got != tt.want {
				t.Fatalf("RouteDictationTranscript(%q, %v) = %q, want %q", tt.text, tt.target, got, tt.want)
			}
		})
	}
}
