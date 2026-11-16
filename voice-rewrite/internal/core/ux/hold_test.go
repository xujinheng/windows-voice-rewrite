package ux

import (
	"testing"
	"time"
)

func TestHoldWindowStopsStayDistinct(t *testing.T) {
	before := HoldEditorReady(false)
	after := HoldCapturedWindow(false)
	closed := ApplyEditorOpen(false)
	if before == nil || after == nil || closed == nil {
		t.Fatal("missing stop")
	}
	seen := map[string]bool{}
	for _, err := range []error{before, after, closed} {
		if seen[err.Error()] {
			t.Fatal("window stops collapsed")
		}
		seen[err.Error()] = true
		if err.Error() == ReplaceWindowSwitched || err.Error() == DictationWindowChanged || err.Error() == ReplaceBeforePaste {
			t.Fatal(err)
		}
		got, ok := Classify(err)
		if !ok || got != selectionMoved {
			t.Fatalf("%v classified as %q %v", err, got, ok)
		}
	}
	if HoldEditorReady(true) != nil || HoldCapturedWindow(true) != nil || ApplyEditorOpen(true) != nil {
		t.Fatal("open window")
	}
	if ApplySettle == 35*time.Millisecond {
		t.Fatal("apply settle matched the copy poll")
	}
}
