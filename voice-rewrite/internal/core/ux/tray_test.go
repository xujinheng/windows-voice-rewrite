package ux

import "testing"

func TestTrayRestAndPauseStayDistinct(t *testing.T) {
	if TrayResetAfter.Milliseconds() != 4500 {
		t.Fatal("reset")
	}
	if TrayAfterTimer(true, false, true) != TrayRestPaused || TrayAfterTimer(false, true, false) != TrayRestIdle || TrayAfterTimer(false, false, true) != TrayRestKeep {
		t.Fatal("rest")
	}
	if ChoosePause(true, false) != PauseRefused || ChoosePause(false, false) != PauseOn || ChoosePause(false, true) != PauseOff {
		t.Fatal("pause")
	}
}
