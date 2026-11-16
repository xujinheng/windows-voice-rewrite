package meeting

import (
	"strings"
	"testing"
)

func TestAcceptProfileKeepsTrimAndRejectsRawLength(t *testing.T) {
	background, terms, err := AcceptProfile("  project  ", "  terms  ")
	if err != nil || background != "project" || terms != "terms" {
		t.Fatalf("trimmed profile: %q %q %v", background, terms, err)
	}
	if _, _, err = AcceptProfile(strings.Repeat("字", MaxBackgroundRunes), strings.Repeat("词", MaxTermsRunes)); err != nil {
		t.Fatal(err)
	}
	_, _, err = AcceptProfile(" "+strings.Repeat("字", MaxBackgroundRunes), "terms")
	if err == nil || err.Error() != "背景最多 4000 字，术语最多 2000 字" {
		t.Fatalf("background length: %v", err)
	}
	_, _, err = AcceptProfile("bg", strings.Repeat("词", MaxTermsRunes+1))
	if err == nil || err.Error() != "背景最多 4000 字，术语最多 2000 字" {
		t.Fatalf("terms length: %v", err)
	}
}

func TestAcceptRefineUsesThreeChoicesAndTwoThousandRunes(t *testing.T) {
	got, ok := AcceptRefine(2, "  shorter please  ")
	if !ok || got != "shorter please" {
		t.Fatalf("accepted refine: %q %v", got, ok)
	}
	if _, ok = AcceptRefine(3, "ok"); ok {
		t.Fatal("fourth reply accepted")
	}
	if _, ok = AcceptRefine(-1, "ok"); ok {
		t.Fatal("negative reply accepted")
	}
	if _, ok = AcceptRefine(0, "   "); ok {
		t.Fatal("blank instruction accepted")
	}
	if _, ok = AcceptRefine(1, strings.Repeat("调", MaxRefineInstructionRunes)); !ok {
		t.Fatal("2000-rune instruction rejected")
	}
	if _, ok = AcceptRefine(1, strings.Repeat("调", MaxRefineInstructionRunes+1)); ok {
		t.Fatal("2001-rune instruction accepted")
	}
}
