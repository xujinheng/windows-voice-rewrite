package voice

import (
	"errors"
	"sync"
	"testing"
)

func readyDeliveryCore(route string) (*Core, uint64) {
	core := &Core{}
	token := core.Apply(Command{Kind: CommandStart}).Token
	core.completed = Result{Text: "shared answer", Title: "Result", Route: route}
	core.completedToken = token
	return core, token
}

func TestDeliveryDecisionsSameForEveryHost(t *testing.T) {
	for _, tc := range []struct {
		route             string
		target, selection Match
		action            DeliveryAction
	}{
		{RouteInsert, MatchSame, MatchUnknown, DeliveryInsert},
		{RouteInsert, MatchDifferent, MatchUnknown, DeliveryShowPreview},
		{RouteInsert, MatchUnknown, MatchSame, DeliveryShowPreview},
		{RouteReplace, MatchSame, MatchSame, DeliveryReplace},
		{RouteReplace, MatchSame, MatchUnknown, DeliveryShowPreview},
		{RouteReplace, MatchDifferent, MatchSame, DeliveryShowPreview},
		{RoutePreview, MatchSame, MatchSame, DeliveryShowPreview},
	} {
		for _, host := range []string{"windows", "macos", "cli"} {
			core, token := readyDeliveryCore(tc.route)
			plan, err := core.PlanDelivery(DeliveryRequest{Token: token,
				Observation: TargetObservation{Target: tc.target, Selection: tc.selection}, Trigger: DeliveryAutomatic})
			if err != nil || plan.Action != tc.action {
				t.Errorf("%s route %s target %d selection %d: %+v %v", host, tc.route, tc.target, tc.selection, plan, err)
			}
			if (plan.AttemptID != 0) != (tc.action != DeliveryShowPreview) {
				t.Errorf("%s preview/write permit mismatch: %+v", host, plan)
			}
		}
	}
}

func TestDeliveryPermitAndReceiptsAreSingleOwner(t *testing.T) {
	core, token := readyDeliveryCore(RouteReplace)
	req := DeliveryRequest{Token: token, Observation: TargetObservation{MatchSame, MatchSame}, Trigger: DeliveryAutomatic}
	plan, err := core.PlanDelivery(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.PlanDelivery(req); !errors.Is(err, ErrAlreadyPlanned) {
		t.Fatalf("duplicate write: %v", err)
	}
	receipt := DeliveryReceipt{Token: token, AttemptID: plan.AttemptID, Status: DeliverySentUnverified}
	for i := 0; i < 2; i++ {
		outcome, err := core.ReportDelivery(receipt)
		if err != nil || outcome != DeliveryUnconfirmed {
			t.Fatalf("unverified %d: %v %v", i, outcome, err)
		}
	}
	if _, err := core.ReportDelivery(DeliveryReceipt{Token: token, AttemptID: plan.AttemptID, Status: DeliveryVerified}); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("contradictory receipt accepted: %v", err)
	}
	req.Trigger = DeliveryUserApply
	if _, err := core.PlanDelivery(req); !errors.Is(err, ErrAlreadyPlanned) {
		t.Fatalf("unverified write retried: %v", err)
	}
	core.Apply(Command{Kind: CommandCancel})
	if _, err := core.PlanDelivery(req); !errors.Is(err, ErrStaleTask) {
		t.Fatalf("stale plan: %v", err)
	}
	if _, err := core.ReportDelivery(receipt); !errors.Is(err, ErrStaleTask) {
		t.Fatalf("stale receipt: %v", err)
	}
}

func TestDeliveryExplicitRetryOnlyAfterNotSent(t *testing.T) {
	core, token := readyDeliveryCore(RouteInsert)
	req := DeliveryRequest{Token: token, Observation: TargetObservation{Target: MatchSame}, Trigger: DeliveryAutomatic}
	first, err := core.PlanDelivery(req)
	if err != nil {
		t.Fatal(err)
	}
	if status, err := core.ReportDelivery(DeliveryReceipt{Token: token, AttemptID: first.AttemptID, Status: DeliveryNotSent}); err != nil || status != DeliveryNeedsPreview {
		t.Fatalf("no-send acknowledgment: %v %v", status, err)
	}
	if _, err := core.PlanDelivery(req); !errors.Is(err, ErrAlreadyPlanned) {
		t.Fatalf("automatic retry: %v", err)
	}
	req.Trigger = DeliveryUserApply
	second, err := core.PlanDelivery(req)
	if err != nil || second.AttemptID == first.AttemptID {
		t.Fatalf("explicit retry: %+v %v", second, err)
	}
}

func TestConcurrentDeliveryOnlyOneWritePermit(t *testing.T) {
	core, token := readyDeliveryCore(RouteInsert)
	req := DeliveryRequest{Token: token, Observation: TargetObservation{Target: MatchSame}, Trigger: DeliveryAutomatic}
	var wg sync.WaitGroup
	var granted int
	var mu sync.Mutex
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if plan, err := core.PlanDelivery(req); err == nil && plan.AttemptID != 0 {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if granted != 1 {
		t.Fatalf("expected one write grant, got %d", granted)
	}
}
