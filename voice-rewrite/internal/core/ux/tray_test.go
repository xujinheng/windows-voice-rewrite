package ux

import "testing"

func TestTrayRestAndPauseStayDistinct(t *testing.T) {
	if !TrayShowsError(true, true, false, true) || TrayShowsError(true, true, true, true) || TrayShowsError(true, false, false, true) || TrayShowsError(true, true, false, false) || TrayShowsError(false, true, false, true) {
		t.Fatal("error pill")
	}
	if !TrayArmsReset(true, false) || !TrayArmsReset(false, true) || TrayArmsReset(false, false) || TrayResetAfter.Milliseconds() != 4500 {
		t.Fatal("reset")
	}
	if TrayAfterTimer(true, false, true) != TrayRestPaused || TrayAfterTimer(false, true, false) != TrayRestIdle || TrayAfterTimer(false, false, true) != TrayRestKeep {
		t.Fatal("rest")
	}
	if ChoosePause(true, false) != PauseRefused || ChoosePause(false, false) != PauseOn || ChoosePause(false, true) != PauseOff {
		t.Fatal("pause")
	}
}
