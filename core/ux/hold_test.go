package ux

import (
	"errors"
	"testing"
	"time"
)

func TestHoldWindowStopsStayDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, message := range []string{HoldWindowChanged, HoldSelectionMoved, ApplyWindowClosed} {
		if seen[message] {
			t.Fatal("window stops collapsed")
		}
		seen[message] = true
		if message == ReplaceWindowSwitched || message == DictationWindowChanged || message == ReplaceBeforePaste {
			t.Fatal(message)
		}
		if got, ok := Classify(errors.New(message)); !ok || got != selectionMoved {
			t.Fatalf("%s classified as %q %v", message, got, ok)
		}
	}
	if ApplySettle == 35*time.Millisecond {
		t.Fatal("apply settle matched the copy poll")
	}
}
