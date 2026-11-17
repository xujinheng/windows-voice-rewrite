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
	Blocked State = iota
	SentUnverified
	Confirmed
	Uncertain
)

type Result struct {
	Kind  Kind
	State State
}

func (r Result) ShowManualResult() bool {
	return r.State == Blocked || r.State == Uncertain
}

// Successful native event injection is not proof that the target editor
// consumed text. Never claim a confirmed edit without an actual read-back.
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
