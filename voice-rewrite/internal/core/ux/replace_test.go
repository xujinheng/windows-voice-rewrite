package ux

import (
	"errors"
	"testing"
)

func TestReplaceInsertStopsBeforeTheClipboard(t *testing.T) {
	if ReplaceReleaseKeys == DictationReleaseKeys || ReplaceRestoreFailed == DictationRestoreFailed {
		t.Fatal("replace sentences collapsed into dictation")
	}
	err := ReplaceSelectionStillThere(errors.New("copy"), "a", "a")
	if err == nil || err.Error() != ReplaceSelectionChanged {
		t.Fatal(err)
	}
	if err = ReplaceSelectionStillThere(nil, "b", "a"); err == nil || err.Error() != ReplaceSelectionChanged {
		t.Fatal(err)
	}
	if err = ReplaceSelectionStillThere(nil, "a", "a"); err != nil {
		t.Fatal(err)
	}
	if err = ReplacePasteKeptSelection(nil, "same", "same"); err == nil || err.Error() != ReplacePasteRefused {
		t.Fatal(err)
	}
	if err = ReplacePasteKeptSelection(errors.New("probe"), "same", "same"); err != nil {
		t.Fatal(err)
	}
}
