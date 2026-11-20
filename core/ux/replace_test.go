package ux

import "testing"

func TestReplacementVerification(t *testing.T) {
	if ReplaceReleaseKeys == DictationReleaseKeys || ReplaceRestoreFailed == DictationRestoreFailed {
		t.Fatal("replace sentences collapsed into dictation")
	}
}
