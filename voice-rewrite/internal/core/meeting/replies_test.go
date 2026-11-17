package meeting

import "testing"

func TestReplyStateKeepsReadableBatchUntilAccepted(t *testing.T) {
	var state ReplyState
	first := state.Next()
	batch := ReplyBatch{Source: "question one"}
	batch.Apply(Line{Kind: "ONE", English: "First", Complete: true})
	if !state.Publish(first, batch) || state.Displayed.Lines[0].English != "First" {
		t.Fatal("first readable suggestion must be displayed")
	}
	batch.Apply(Line{Kind: "TWO", English: "Second", Complete: true})
	state.Publish(first, batch)
	if state.Displayed.Lines[1].English != "Second" || state.PendingAvailable {
		t.Fatal("remaining options must stream into the displayed generation")
	}
	next := state.Next()
	incoming := ReplyBatch{Source: "question two", Tip: "think first"}
	incoming.Apply(Line{Kind: "ONE", English: "Incoming", Complete: false})
	state.Publish(next, incoming)
	if state.PendingAvailable || state.AcceptPending() {
		t.Fatal("partial first suggestion must not be selectable")
	}
	incoming.Apply(Line{Kind: "ONE", English: "Incoming", Complete: true})
	if !state.Publish(next, incoming) || !state.PendingAvailable || state.Displayed.Lines[0].English != "First" {
		t.Fatal("new response must wait until explicitly accepted")
	}
	if !state.AcceptPending() || state.PendingAvailable || state.Displayed.Lines[0].English != "Incoming" || state.Displayed.Source != "question two" || state.Displayed.Tip != "think first" {
		t.Fatalf("accepting a finished suggestion failed: %+v", state)
	}
	incoming.Apply(Line{Kind: "THREE", English: "Third", Complete: true})
	state.Publish(next, incoming)
	if state.Displayed.Lines[2].English != "Third" || state.PendingAvailable {
		t.Fatal("accepted batch must continue receiving later options")
	}
}

func TestReplyStateRejectsSupersededGeneration(t *testing.T) {
	var state ReplyState
	old := state.Next()
	state.Publish(old, ReplyBatch{Lines: [3]Line{{English: "Old", Complete: true}}})
	next := state.Next()
	state.Publish(next, ReplyBatch{Lines: [3]Line{{English: "New", Complete: true}}})
	if !state.PendingAvailable {
		t.Fatal("new suggestion must be available")
	}
	current := state.Displayed
	if state.Publish(old, ReplyBatch{Lines: [3]Line{{English: "Stale", Complete: true}}}) {
		t.Fatal("superseded generation accepted")
	}
	if state.Displayed != current || state.Pending.Lines[0].English != "New" {
		t.Fatal("stale generation displaced a readable reply")
	}
	newest := state.Next()
	if newest == next || state.PendingAvailable || state.AcceptPending() {
		t.Fatal("obsolete pending suggestion remained available")
	}
}

func TestReplyStateResetPreservesCompletedChoices(t *testing.T) {
	var state ReplyState
	serial := state.Next()
	batch := ReplyBatch{Lines: [3]Line{
		{English: "Partial"},
		{English: "Complete", Chinese: "已完成", Complete: true},
	}}
	state.Publish(serial, batch)
	state.ResetWork()
	if state.Displayed.Lines[0].English != "" || state.Displayed.Lines[1].English != "Complete" {
		t.Fatal("reset did not discard partial output and keep the last completed choice")
	}
	if state.Current(serial) || state.PendingAvailable {
		t.Fatal("reset did not invalidate obsolete callbacks")
	}
	if state.Publish(serial, batch) {
		t.Fatal("canceled work was accepted")
	}
}

func TestReplyStatePendingFailureKeepsCurrentAndFinishedPending(t *testing.T) {
	var state ReplyState
	original := state.Next()
	state.Publish(original, ReplyBatch{Lines: [3]Line{{English: "Readable", Complete: true}}})
	next := state.Next()
	state.Publish(next, ReplyBatch{Lines: [3]Line{
		{English: "Ready", Complete: true},
		{English: "unfinished"},
	}})
	state.DiscardPartial()
	if !state.PendingAvailable || state.Pending.Lines[1].English != "" || state.Displayed.Lines[0].English != "Readable" {
		t.Fatal("failed generation erased a completed pending option or active reply")
	}
}

func TestReplyBatchAppliesKnownProtocolLines(t *testing.T) {
	var batch ReplyBatch
	for _, line := range []Line{
		{Kind: "ONE", English: "one"},
		{Kind: "TWO", English: "two"},
		{Kind: "THREE", English: "three"},
		{Kind: "TIP", Chinese: "提示"},
	} {
		if !batch.Apply(line) {
			t.Fatalf("ignored known protocol line %s", line.Kind)
		}
	}
	if batch.Lines[0].English != "one" || batch.Lines[1].English != "two" || batch.Lines[2].English != "three" || batch.Tip != "提示" {
		t.Fatalf("protocol line mapped to wrong slot: %+v", batch)
	}
	if batch.Apply(Line{Kind: "OTHER", English: "bad"}) {
		t.Fatal("unknown protocol changed a reply batch")
	}
}

func TestReplyWorkCoalescesAutomaticAndManualAndAllowsExplicitRetry(t *testing.T) {
	var state ReplyState
	if !state.MayStartTurn(17, true) {
		t.Fatal("first automatic turn rejected")
	}
	first := state.StartTurn(17)
	if state.MayStartTurn(17, true) || state.MayStartTurn(17, false) {
		t.Fatal("duplicate inference allowed during an active turn")
	}
	next := state.StartTurn(18)
	state.FinishTurn(first, true)
	if !state.WorkActive || state.WorkTurn != 18 || state.CompletedTurn == 17 {
		t.Fatal("stale completion changed current work")
	}
	state.FinishTurn(next, true)
	if state.MayStartTurn(18, true) || !state.MayStartTurn(18, false) ||
		!state.MayStartTurn(19, true) {
		t.Fatal("completed turn should suppress automatic repeats but allow a manual refresh")
	}
	retry := state.StartTurn(18)
	state.FinishTurn(retry, false)
	if !state.MayStartTurn(18, false) {
		t.Fatal("failed manual request could not be retried")
	}
	state.StartTurn(19)
	state.ResetWork()
	if state.WorkActive || !state.MayStartTurn(19, false) {
		t.Fatal("cancellation left the reply state permanently busy")
	}
}
