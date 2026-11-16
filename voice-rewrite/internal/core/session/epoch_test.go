package session

import (
	"testing"
)

func TestEpochRejectsOldTask(t *testing.T) {
	var g Epoch
	first := g.Start()
	if !g.Valid(first) {
		t.Fatal("first task should be current")
	}
	next := g.Start()
	if g.Valid(first) || !g.Valid(next) {
		t.Fatal("a new task must fence old workers")
	}
	g.Shutdown()
	if g.Valid(next) || g.Start() != 0 {
		t.Fatal("shutdown must fence every task")
	}
}

func TestEpochCancelsPriorRequests(t *testing.T) {
	var g Epoch
	first := g.Start()
	previous := g.ContextFor(first)
	select {
	case <-previous.Done():
		t.Fatal("current task should not be canceled")
	default:
	}
	second := g.Start()
	select {
	case <-previous.Done():
	default:
		t.Fatal("starting a new task must cancel previous requests")
	}
	if !g.Valid(second) || g.Valid(first) {
		t.Fatal("old generation must not be valid")
	}
	select {
	case <-g.ContextFor(first).Done():
	default:
		t.Fatal("stale generation context must be canceled")
	}
	g.Shutdown()
	select {
	case <-g.ContextFor(second).Done():
	default:
		t.Fatal("shutdown must cancel active requests")
	}
}
