// Package delivery defines OS-independent voice output semantics.
// Hosts own native input and decide whether an editor can receive an event.
package delivery

type Kind uint8

const (
	Dictation Kind = iota
	ReplaceSelection
)

type State uint8

const (
	Blocked State = 0
	SentUnverified State = 1
	Confirmed State = 2
	Uncertain State = 3
)

type Result struct {
	Kind Kind
	State State
}

func (r Result) ShowManualResult() bool {
	return r.State == Blocked || r.State == Uncertain
}

func Sent(kind Kind) Result {
	return Result{Kind: kind, State: SentUnverified}
}

func Verified(kind Kind) Result {
	return Result{Kind: kind, State: Confirmed}
}

func Failure(kind Kind, mayHaveSent bool) Result {
	if mayHaveSent {
		return Result{Kind: kind, State: Uncertain}
	}
	return Result{Kind: kind, State: Blocked}
}
