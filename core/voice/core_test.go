package voice

import (
	"testing"
)

func TestCoreRejectsOldTask(t *testing.T) {
	var g Core
	first := g.Apply(Command{Kind: CommandStart}).Token
	if !g.Snapshot(0, first).Valid {
		t.Fatal("first task should be current")
	}
	next := g.Apply(Command{Kind: CommandStart}).Token
	if g.Snapshot(0, first).Valid || !g.Snapshot(0, next).Valid {
		t.Fatal("a new task must fence old workers")
	}
	g.Apply(Command{Kind: CommandShutdown})
	if g.Snapshot(0, next).Valid || g.Apply(Command{Kind: CommandStart}).Token != 0 {
		t.Fatal("shutdown must fence every task")
	}
}

func TestCoreCancelsPriorRequests(t *testing.T) {
	var g Core
	first := g.Apply(Command{Kind: CommandStart}).Token
	previous := g.ContextFor(first)
	select {
	case <-previous.Done():
		t.Fatal("current task should not be canceled")
	default:
	}
	second := g.Apply(Command{Kind: CommandStart}).Token
	select {
	case <-previous.Done():
	default:
		t.Fatal("starting a new task must cancel previous requests")
	}
	if !g.Snapshot(0, second).Valid || g.Snapshot(0, first).Valid {
		t.Fatal("old generation must not be valid")
	}
	select {
	case <-g.ContextFor(first).Done():
	default:
		t.Fatal("stale generation context must be canceled")
	}
	g.Apply(Command{Kind: CommandShutdown})
	select {
	case <-g.ContextFor(second).Done():
	default:
		t.Fatal("shutdown must cancel active requests")
	}
}

func TestPauseIsOneCoreOwnedInputPolicy(t *testing.T) {
	var g Core
	g.Apply(Command{Kind: CommandBind, Dictation: []int{1}, Rewrite: []int{2}})
	if got := g.Apply(Command{Kind: CommandPause}); got.Kind != EventPaused || !g.Snapshot(0, 0).Paused {
		t.Fatal("pause must be visible to every native adapter")
	}
	for _, code := range []int{1, 2} {
		if event := g.Apply(Command{Kind: CommandPress, Code: code}); event.Kind != EventRejected {
			t.Fatal("paused Core accepted a voice press")
		}
	}
	if event := g.Apply(Command{Kind: CommandStart}); event.Kind != EventRejected {
		t.Fatal("paused Core started a task")
	}
	if g.Apply(Command{Kind: CommandResume, Busy: true}).Kind != EventRejected { t.Fatal("busy resume accepted") }
	if got := g.Apply(Command{Kind: CommandResume}); got.Kind != EventResumed || g.Snapshot(0, 0).Paused {
		t.Fatal("resume must unlock both clients")
	}
	if g.Apply(Command{Kind: CommandPause, Busy: true}).Kind != EventRejected { t.Fatal("busy pause accepted") }
	press := g.Apply(Command{Kind: CommandPress, Code: 1})
	if press.Kind != EventPressed || g.Apply(Command{Kind: CommandPause}).Kind != EventRejected {
		t.Fatal("cannot pause while a shortcut owns a hold")
	}
}
