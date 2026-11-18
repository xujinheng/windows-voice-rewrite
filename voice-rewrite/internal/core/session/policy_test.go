package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEpochCancellationAndStageClock(t *testing.T) {
	var g Epoch
	first := g.Start()
	ctx := g.ContextFor(first)
	if !g.StartStage(first, StageASR, 2*time.Millisecond) {
		t.Fatal("stage did not start")
	}
	time.Sleep(5 * time.Millisecond)
	if g.Valid(first) || g.Reason() != ReasonTimeout {
		t.Fatal("expired stage must revoke current task")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("provider context not canceled by stage timeout")
	}
	second := g.Start()
	if second <= first || !g.Valid(second) || g.Cancel(first, ReasonUser) {
		t.Fatal("old epoch must not cancel a replacement")
	}
	if !g.StartStage(second, StageNativeWrite, time.Second) ||
		!g.FinishStage(second, StageNativeWrite) || !g.Valid(second) {
		t.Fatal("finishing a successful stage must disarm the watchdog")
	}
	if !g.Cancel(second, ReasonUser) || g.Cancel(second, ReasonUser) || g.Valid(second) {
		t.Fatal("cancel must be idempotent and revoke exactly one epoch")
	}
	if g.ContextFor(second).Err() != context.Canceled {
		t.Fatal("old context must be canceled")
	}
}

func TestEpochRecordingLimitPreservesContext(t *testing.T) {
	var g Epoch
	id := g.Start()
	ctx := g.ContextFor(id)
	if !g.StartStage(id, StageRecording, time.Millisecond) {
		t.Fatal("record start")
	}
	time.Sleep(3 * time.Millisecond)
	if !g.RecordingLimit(id) || !g.Valid(id) || ctx.Err() != nil {
		t.Fatal("missing physical release must preserve recorded audio and task")
	}
	if g.RecordingLimit(id) || !g.StartStage(id, StageCaptureStop, time.Second) {
		t.Fatal("duplicate record limit or capture stop rejected")
	}
}

func TestPolicyBudgets(t *testing.T) {
	cases := []struct {
		stage          Stage
		bytes, seconds int
		expected       time.Duration
	}{
		{StageMicrophoneStart, 0, 0, 30 * time.Second},
		{StageASR, 0, 0, 80 * time.Second},
		{StageASR, 32000 * 180, 0, 235 * time.Second},
		{StageSpokenEdit, 0, 0, 375 * time.Second},
		{StageSelectedEdit, 0, 0, 165 * time.Second},
		{StageNativeWrite, 0, 0, 20 * time.Second},
		{StageRecording, 0, 180, 182 * time.Second},
		{"not_a_stage", 0, 0, 0},
	}
	for _, tt := range cases {
		if got := StageBudget(tt.stage, tt.bytes, tt.seconds); got != tt.expected {
			t.Errorf("%s got %s want %s", tt.stage, got, tt.expected)
		}
	}
}

func TestPolicyCancelLateCallbackAndABA(t *testing.T) {
	var p Policy
	start := Event{Type: EventBegin, SessionID: "task-A", Generation: 1}
	if !p.Apply(start).Allowed {
		t.Fatal("start A")
	}
	if !p.Apply(Event{Type: EventStage, SessionID: "task-A", Generation: 1, Stage: StageASR}).Allowed {
		t.Fatal("ASR stage")
	}
	if !p.Apply(Event{Type: EventBind, SessionID: "task-A", Generation: 1, RequestID: "asr-42"}).Allowed {
		t.Fatal("bind provider id")
	}
	canceled := p.Apply(Event{Type: EventCancel, SessionID: "task-A", Generation: 1, Reason: ReasonUser})
	if !canceled.Allowed || len(canceled.Effects) != 4 ||
		canceled.Effects[0].Type != "cancel_request" || canceled.Effects[0].RequestID != "asr-42" {
		t.Fatalf("wrong cancellation effects: %+v", canceled)
	}
	seen := make(map[string]bool)
	for _, effect := range canceled.Effects {
		if seen[effect.ID] || !strings.Contains(effect.ID, "task-A/1/") {
			t.Fatalf("effect id collision: %+v", effect)
		}
		seen[effect.ID] = true
	}
	if got := p.Apply(Event{Type: EventCancel, SessionID: "task-A", Generation: 1}); got.Allowed || len(got.Effects) > 0 {
		t.Fatal("duplicate cancel must not rerun native effects")
	}
	if !p.Apply(Event{Type: EventBegin, SessionID: "task-B", Generation: 2}).Allowed {
		t.Fatal("new task should start immediately")
	}
	late := []Event{
		{Type: EventAuthorize, SessionID: "task-A", Generation: 1, Stage: StageASR},
		{Type: EventReceipt, SessionID: "task-A", Generation: 1, Stage: StageNativeWrite},
		{Type: EventTimeout, SessionID: "task-A", Generation: 1, Stage: StageASR},
		{Type: EventCancel, SessionID: "task-A", Generation: 1},
		{Type: EventBegin, SessionID: "task-A", Generation: 3},
		{Type: EventBegin, SessionID: "task-B", Generation: 4},
	}
	for _, event := range late {
		got := p.Apply(event)
		if got.Allowed || len(got.Effects) > 0 {
			t.Fatalf("stale/ABA event accepted: %+v", event)
		}
	}
	if !p.Apply(Event{Type: EventStage, SessionID: "task-B", Generation: 2, Stage: StageNativeWrite}).Allowed {
		t.Fatal("old callbacks affected new task")
	}
	if !p.Apply(Event{Type: EventReceipt, SessionID: "task-B", Generation: 2, Stage: StageNativeWrite}).Allowed {
		t.Fatal("successful native write receipt")
	}
	if got := p.Apply(Event{Type: EventTimeout, SessionID: "task-B", Generation: 2, Stage: StageNativeWrite}); got.Allowed || len(got.Effects) > 0 {
		t.Fatal("timeout after success must not cancel")
	}
}

func TestPolicySameTraceBothHosts(t *testing.T) {
	trace := []Event{
		{Type: EventBegin, SessionID: "native-1", Generation: 1},
		{Type: EventStage, SessionID: "native-1", Generation: 1, Stage: StageMicrophoneStart},
		{Type: EventStage, SessionID: "native-1", Generation: 1, Stage: StageASR, AudioBytes: 32000},
		{Type: EventBind, SessionID: "native-1", Generation: 1, RequestID: "asr"},
		{Type: EventCancel, SessionID: "native-1", Generation: 1, Reason: ReasonFocusChanged},
		{Type: EventAuthorize, SessionID: "native-1", Generation: 1, Stage: StageASR},
		{Type: EventBegin, SessionID: "native-2", Generation: 2},
		{Type: EventTimeout, SessionID: "native-1", Generation: 1, Stage: StageASR},
	}
	var windows, mac Policy
	for i, event := range trace {
		a, b := windows.Apply(event), mac.Apply(event)
		if a.Allowed != b.Allowed || len(a.Effects) != len(b.Effects) ||
			a.Reason != b.Reason || a.BudgetMilliseconds != b.BudgetMilliseconds {
			t.Fatalf("cross-host trace diverged step %d: %+v / %+v", i, a, b)
		}
		for j := range a.Effects {
			if a.Effects[j] != b.Effects[j] {
				t.Fatalf("effect mismatch at %d", i)
			}
		}
	}
}

func TestPolicyConcurrentOldReceipts(t *testing.T) {
	var p Policy
	p.Apply(Event{Type: EventBegin, SessionID: "old", Generation: 1})
	p.Apply(Event{Type: EventStage, SessionID: "old", Generation: 1, Stage: StageModel})
	p.Apply(Event{Type: EventBegin, SessionID: "new", Generation: 2})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := p.Apply(Event{Type: EventReceipt, SessionID: "old", Generation: 1, Stage: StageModel}); got.Allowed {
				t.Error("old receipt accepted")
			}
		}()
	}
	wg.Wait()
	if !p.Apply(Event{Type: EventAuthorize, SessionID: "new", Generation: 2}).Allowed {
		t.Fatal("new active task lost")
	}
}

func TestPolicyManySequentialTasksKeepNewGenerationAvailable(t *testing.T) {
	var p Policy
	for i := uint64(1); i <= 65537; i++ {
		id := fmt.Sprintf("session-%d", i)
		if !p.Apply(Event{Type: EventBegin, SessionID: id, Generation: i}).Allowed {
			t.Fatalf("fresh task %d rejected; session cap must not lock out users", i)
		}
		if !p.Apply(Event{Type: EventCancel, SessionID: id, Generation: i}).Allowed {
			t.Fatalf("cancel task %d failed", i)
		}
	}
}
