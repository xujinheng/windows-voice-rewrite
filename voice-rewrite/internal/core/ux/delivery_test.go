package ux

import (
	"errors"
	"testing"

	"voicerewrite/internal/core/indicator"
)

func TestDictationDeliveryKeepsTheCardAndPillPair(t *testing.T) {
	failed := DictationInserted(errors.New("paste"))
	if !failed.ShowCard || failed.Title != DictationCopyManually || failed.Phase != indicator.Fallback {
		t.Fatalf("failed %+v", failed)
	}
	sent := DictationInserted(nil)
	if sent.ShowCard || sent.Phase != indicator.Success || !sent.TraySuccess {
		t.Fatalf("sent %+v", sent)
	}
	applied := RewritePasted(true, nil)
	if applied.ShowCard || applied.Phase != indicator.Success || !applied.SetPhase {
		t.Fatalf("applied %+v", applied)
	}
	preview := RewritePasted(false, nil)
	if !preview.ShowCard || preview.SetPhase || !preview.TraySuccess {
		t.Fatalf("preview %+v", preview)
	}
	missed := RewritePasted(true, errors.New("paste"))
	if !missed.ShowCard || missed.SetPhase || missed.Phase != 0 {
		t.Fatalf("missed %+v", missed)
	}
}

func TestDictationDeliveryUncertaintyIsVisibleButNeverRetried(t *testing.T) {
	// A paste/restore error may happen after SendInput fired. The result card
	// must be shown without showing a successful tray indication or retry path.
	failed := DictationInserted(errors.New("paste may have been sent"))
	if failed != (Delivery{ShowCard: true, Title: DictationCopyManually, Phase: indicator.Fallback, SetPhase: true}) {
		t.Fatalf("uncertain paste was reported as delivered: %+v", failed)
	}
	sent := DictationInserted(nil)
	if sent != (Delivery{Phase: indicator.Success, SetPhase: true, TraySuccess: true}) {
		t.Fatalf("accepted SendInput must keep its existing outcome: %+v", sent)
	}
}
