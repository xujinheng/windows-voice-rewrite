package voiceflow

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

func event(kind EventType, gen uint64, n int) Event {
	return Event{Version: Version, Type: kind, SessionID: "test", Generation: gen, CommandID: fmt.Sprintf("event-%d", n)}
}

func next(t *testing.T, engine *Engine, ev Event) Transition {
	t.Helper()
	got, err := engine.Apply(ev)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Accepted {
		t.Fatalf("%s rejected: %+v", ev.Type, got)
	}
	return got
}

func effect(t *testing.T, result Transition, kind ActionType) Action {
	t.Helper()
	for _, a := range result.Effects {
		if a.Type == kind {
			return a
		}
	}
	t.Fatalf("missing %s: %+v", kind, result)
	return Action{}
}

func receipt(ev Event, a Action, ok bool) Event {
	ev.RequestID = a.RequestID
	ev.ReceiptID = a.CommandID
	ev.Success = ok
	return ev
}

func TestDictationHoldsWriteUntilMatchedReceipt(t *testing.T) {
	e := New()
	arm := event(RecordArm, 1, 1)
	arm.Mode, arm.CanInsert = Dictation, true
	effect(t, next(t, e, arm), ShowPreview)
	start := effect(t, next(t, e, event(Press, 1, 2)), StartNativeCapture)
	next(t, e, receipt(event(RecordReady, 1, 3), start, true))
	stop := effect(t, next(t, e, event(Release, 1, 4)), StopNativeCapture)
	asr := effect(t, next(t, e, receipt(event(AudioStopped, 1, 5), stop, true)), SendASR)
	speech := receipt(event(ASRResult, 1, 6), asr, true)
	speech.Text = "  中文 Hello  "
	write := effect(t, next(t, e, speech), AttemptNativeWrite)
	if write.Text != "中文 Hello" || write.Generation != 1 || write.CommandID == "" {
		t.Fatalf("write effect lost identity or transcript: %+v", write)
	}
	duplicate, err := e.Apply(speech)
	if err != nil || !duplicate.Duplicate || len(duplicate.Effects) != 0 {
		t.Fatalf("duplicate ASR issued effects: %+v %v", duplicate, err)
	}
	falseReceipt := receipt(event(WriteReceipt, 1, 7), asr, true)
	if result, err := e.Apply(falseReceipt); err != nil || result.Accepted || len(result.Effects) != 0 {
		t.Fatalf("wrong receipt accepted: %+v %v", result, err)
	}
	done := next(t, e, receipt(event(WriteReceipt, 1, 8), write, true))
	if done.Phase != Success {
		t.Fatalf("expected confirmed success: %+v", done)
	}
	effect(t, done, AppendHistory)
	effect(t, done, ClearTask)
	late, err := e.Apply(receipt(event(WriteReceipt, 1, 9), write, true))
	if err != nil || late.Accepted || len(late.Effects) != 0 {
		t.Fatalf("late receipt mutated terminal state: %+v %v", late, err)
	}
}

func TestEarlyReleaseLimitAndUncertainWrite(t *testing.T) {
	for _, end := range []EventType{Release, RecordLimit} {
		t.Run(string(end), func(t *testing.T) {
			e := New()
			arm := event(RecordArm, 1, 1)
			arm.Mode, arm.CanInsert = Dictation, true
			next(t, e, arm)
			start := effect(t, next(t, e, event(Press, 1, 2)), StartNativeCapture)
			pending := next(t, e, event(end, 1, 3))
			if len(pending.Effects) != 0 {
				t.Fatalf("stop before native start receipt: %+v", pending)
			}
			stop := effect(t, next(t, e, receipt(event(RecordReady, 1, 4), start, true)), StopNativeCapture)
			if stop.CommandID == start.CommandID {
				t.Fatal("start and stop share effect identity")
			}
			asr := effect(t, next(t, e, receipt(event(AudioStopped, 1, 5), stop, true)), SendASR)
			result := receipt(event(ASRResult, 1, 6), asr, true)
			result.Text = "Speech"
			write := effect(t, next(t, e, result), AttemptNativeWrite)
			fallback := next(t, e, receipt(event(WriteReceipt, 1, 7), write, false))
			effect(t, fallback, ShowPreview)
			if fallback.Phase != Preview {
				t.Fatalf("uncertain paste must not be successful: %+v", fallback)
			}
			for _, a := range fallback.Effects {
				if a.Type == AttemptNativeWrite || a.Type == AppendHistory {
					t.Fatalf("uncertain write generated retry/success: %+v", fallback)
				}
			}
		})
	}
}

func TestCancelFocusTimeoutAndLateResults(t *testing.T) {
	for _, ending := range []EventType{Cancel, FocusChanged, Timeout} {
		t.Run(string(ending), func(t *testing.T) {
			e := New()
			arm := event(RecordArm, 8, 1)
			arm.Mode, arm.CanInsert = Dictation, true
			next(t, e, arm)
			start := effect(t, next(t, e, event(Press, 8, 2)), StartNativeCapture)
			next(t, e, receipt(event(RecordReady, 8, 3), start, true))
			stop := effect(t, next(t, e, event(Release, 8, 4)), StopNativeCapture)
			asr := effect(t, next(t, e, receipt(event(AudioStopped, 8, 5), stop, true)), SendASR)
			end := event(ending, 8, 6)
			if ending == Timeout { end.TimeoutOrigin = "asr_deadline" }
			done := next(t, e, end)
			if done.Phase != Canceled {
				t.Fatalf("cancel did not win: %+v", done)
			}
			for _, a := range done.Effects {
				if a.Type == AttemptNativeWrite {
					t.Fatalf("cancel issued native write: %+v", done)
				}
			}
			late := receipt(event(ASRResult, 8, 7), asr, true)
			late.Text = "must not insert"
			rejected, err := e.Apply(late)
			if err != nil || rejected.Accepted || len(rejected.Effects) != 0 {
				t.Fatalf("post-cancel result was accepted: %+v %v", rejected, err)
			}
			arm2 := event(RecordArm, 9, 8)
			arm2.Mode = Dictation
			next(t, e, arm2)
			stale, err := e.Apply(event(Press, 8, 9))
			if err != nil || stale.Accepted || len(stale.Effects) != 0 {
				t.Fatalf("old generation reached new session: %+v %v", stale, err)
			}
		})
	}
}

func TestRewriteModelRouteAndPreview(t *testing.T) {
	for _, canInsert := range []bool{false, true} {
		t.Run(fmt.Sprint(canInsert), func(t *testing.T) {
			e := New()
			arm := event(RecordArm, 1, 1)
			arm.Mode, arm.CanInsert = Rewrite, canInsert
			next(t, e, arm)
			start := effect(t, next(t, e, event(Press, 1, 2)), StartNativeCapture)
			next(t, e, receipt(event(RecordReady, 1, 3), start, true))
			stop := effect(t, next(t, e, event(RecordLimit, 1, 4)), StopNativeCapture)
			asr := effect(t, next(t, e, receipt(event(AudioStopped, 1, 5), stop, true)), SendASR)
			asrReply := receipt(event(ASRResult, 1, 6), asr, true)
			asrReply.Text = "rewrite selected"
			model := effect(t, next(t, e, asrReply), SendModel)
			reply := receipt(event(ModelResult, 1, 7), model, true)
			reply.Text, reply.Authorized = "New text", true
			got := next(t, e, reply)
			if canInsert {
				effect(t, got, AttemptNativeWrite)
			} else {
				effect(t, got, ShowPreview)
				if got.Phase != Preview { t.Fatal(got) }
			}
		})
	}
}

func TestOutOfOrderDuplicateAndBadEnvelopes(t *testing.T) {
	e := New()
	arm := event(RecordArm, 3, 1)
	arm.Mode = Dictation
	next(t, e, arm)
	for _, bad := range []Event{
		{Version: 2, Type: Press, SessionID: "test", Generation: 3, CommandID: "bad"},
		{Version: 1, Type: Press, SessionID: "test", Generation: 0, CommandID: "bad"},
		{Version: 1, Type: Timeout, SessionID: "test", Generation: 3, CommandID: "bad"},
	} {
		if _, err := e.Apply(bad); err == nil {
			t.Fatalf("accepted malformed %+v", bad)
		}
	}
	early := event(ASRResult, 3, 2)
	early.Text = "untrusted"
	if got, err := e.Apply(early); err != nil || got.Accepted || len(got.Effects) != 0 {
		t.Fatalf("early provider reply accepted: %+v %v", got, err)
	}
	start := next(t, e, event(Press, 3, 3))
	effect(t, start, StartNativeCapture)
	duplicated, err := e.Apply(event(Press, 3, 3))
	if err != nil || !duplicated.Duplicate { t.Fatalf("repeat press %+v %v", duplicated, err) }
	foreign := event(RecordArm, 3, 9)
	foreign.Mode = Dictation
	if got, err := e.Apply(foreign); err != nil || got.Accepted { t.Fatalf("reused generation %+v %v", got, err) }
}

func TestConcurrentReplaysNeverDuplicateCapture(t *testing.T) {
	e := New()
	arm := event(RecordArm, 4, 1)
	arm.Mode = Dictation
	next(t, e, arm)
	const count = 128
	var wg sync.WaitGroup
	outputs := make(chan Transition, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := e.Apply(event(Press, 4, 2))
			if err != nil { t.Error(err) }
			outputs <- out
		}()
	}
	wg.Wait()
	close(outputs)
	captures := 0
	for result := range outputs {
		for _, a := range result.Effects {
			if a.Type == StartNativeCapture { captures++ }
		}
	}
	if captures != 1 { t.Fatalf("expected one native capture, got %d", captures) }
}

func TestEventJSONV1(t *testing.T) {
	e := event(RecordArm, 1, 1)
	e.Mode = Dictation
	data, err := json.Marshal(e)
	if err != nil { t.Fatal(err) }
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil { t.Fatal(err) }
	if decoded != e { t.Fatalf("event JSON not stable: %+v", decoded) }
}
