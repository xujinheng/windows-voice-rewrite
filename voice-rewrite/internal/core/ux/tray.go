package ux

import "time"

const TrayResetAfter = 4500 * time.Millisecond

type TrayRest int

const (
	TrayRestKeep TrayRest = iota
	TrayRestPaused
	TrayRestIdle
)

// TrayAfterTimer returns the resting tray state for a finished success or error.
func TrayAfterTimer(isSuccess, isError, paused bool) TrayRest {
	if !isSuccess && !isError {
		return TrayRestKeep
	}
	if paused {
		return TrayRestPaused
	}
	return TrayRestIdle
}

type PauseChoice int

const (
	PauseRefused PauseChoice = iota
	PauseOn
	PauseOff
)

// ChoosePause refuses a pause while work is running. Otherwise it flips the current pause.
func ChoosePause(busy, paused bool) PauseChoice {
	if busy {
		return PauseRefused
	}
	if paused {
		return PauseOff
	}
	return PauseOn
}
