package ux

import (
	"errors"
	"testing"
)

func TestReplaceInsertStopsBeforeTheClipboard(t *testing.T) {
	if ReplaceReleaseKeys == DictationReleaseKeys || ReplaceRestoreFailed == DictationRestoreFailed {
		t.Fatal("replace sentences collapsed into dictation")
	}
	called := 0
	err := ReplaceInsertReady(func() bool { return false }, func() bool { called++; return true }, func() bool { called++; return true }, func() bool { called++; return true })
	if err == nil || err.Error() != ReplaceExpired || called != 0 {
		t.Fatalf("expired %v %d", err, called)
	}
	err = ReplaceInsertReady(func() bool { return true }, func() bool { return false }, func() bool { t.Fatal("window"); return true }, func() bool { return true })
	if err == nil || err.Error() != ReplaceReleaseKeys {
		t.Fatal(err)
	}
	err = ReplaceInsertReady(func() bool { return true }, func() bool { return true }, func() bool { return false }, func() bool { t.Fatal("password"); return true })
	if err == nil || err.Error() != ReplaceWindowSwitched {
		t.Fatal(err)
	}
	err = ReplaceInsertReady(func() bool { return true }, func() bool { return true }, func() bool { return true }, func() bool { return true })
	if err == nil || err.Error() != PasswordRewrite {
		t.Fatal(err)
	}
	if err = ReplaceInsertReady(func() bool { return true }, func() bool { return true }, func() bool { return true }, func() bool { return false }); err != nil {
		t.Fatal(err)
	}
	if err = ReplaceSelectionStillThere(errors.New("copy"), "a", "a"); err == nil || err.Error() != ReplaceSelectionChanged {
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

func TestSelectionReadReady(t *testing.T) {
	if err := SelectionReadReady(false, func() bool { t.Fatal("probe without target"); return false }); err == nil {
		t.Fatal("missing target accepted")
	}
	if err := SelectionReadReady(true, func() bool { return true }); err == nil || err.Error() != PasswordSelection {
		t.Fatal("secure selection accepted:", err)
	}
	if err := SelectionReadReady(true, func() bool { return false }); err != nil {
		t.Fatal("ordinary selection rejected:", err)
	}
}
