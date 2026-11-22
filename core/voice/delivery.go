package voice

import "errors"

// A host reports OS observations, never its own rewrite/preview policy.
type Match uint8

const (
	MatchUnknown Match = iota
	MatchSame
	MatchDifferent
)

type TargetObservation struct {
	Target    Match
	Selection Match
}

type DeliveryTrigger uint8

const (
	DeliveryAutomatic DeliveryTrigger = iota + 1
	DeliveryUserApply
)

type DeliveryAction uint8

const (
	DeliveryShowPreview DeliveryAction = iota + 1
	DeliveryInsert
	DeliveryReplace
)

type DeliveryRequest struct {
	Token       uint64
	Observation TargetObservation
	Trigger     DeliveryTrigger
}

type DeliveryPlan struct {
	AttemptID uint64 `json:"attemptID"`
	Action    DeliveryAction `json:"action"`
	Text      string `json:"text"`
	Title     string `json:"title"`
}

type DeliveryReceiptStatus uint8

const (
	DeliveryNotSent DeliveryReceiptStatus = iota + 1
	DeliverySentUnverified
	DeliveryVerified
	DeliveryUnknown
)

type DeliveryReceipt struct {
	Token     uint64
	AttemptID uint64
	Status    DeliveryReceiptStatus
}

type DeliveryOutcome uint8

const (
	DeliveryNeedsPreview DeliveryOutcome = iota + 1
	DeliveryCompleted
	DeliveryUnconfirmed
)

var (
	ErrAlreadyPlanned = errors.New("delivery already planned")
	ErrDeliveryConflict = errors.New("conflicting delivery receipt")
)

// PlanDelivery is the sole portable choice between insert, replace and preview.
// Granting a write plan consumes the task's one automatic write attempt.
func (c *Core) PlanDelivery(req DeliveryRequest) (DeliveryPlan, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || req.Token == 0 || c.currentID != req.Token || c.completedToken != req.Token {
		return DeliveryPlan{}, ErrStaleTask
	}
	if req.Trigger != DeliveryAutomatic && req.Trigger != DeliveryUserApply {
		return DeliveryPlan{}, ErrInvalidTask
	}
	if req.Observation.Target > MatchDifferent || req.Observation.Selection > MatchDifferent {
		return DeliveryPlan{}, ErrInvalidTask
	}
	plan := DeliveryPlan{Action: DeliveryShowPreview, Text: c.completed.Text, Title: c.completed.Title}
	if req.Observation.Target == MatchSame {
		switch c.completed.Route {
		case RouteInsert:
			plan.Action = DeliveryInsert
		case RouteReplace:
			if req.Observation.Selection == MatchSame {
				plan.Action = DeliveryReplace
			}
		}
	}
	if plan.Action == DeliveryShowPreview {
		return plan, nil
	}
	// A confirmed NotSent can be retried only by an explicit user action.
	if c.deliveryAttempt != 0 && !(req.Trigger == DeliveryUserApply && c.deliveryStatus == DeliveryNotSent) {
		return DeliveryPlan{}, ErrAlreadyPlanned
	}
	c.deliverySequence++
	c.deliveryAttempt = c.deliverySequence
	c.deliveryStatus = 0
	plan.AttemptID = c.deliveryAttempt
	return plan, nil
}

// ReportDelivery is idempotent for identical receipts and rejects contradictions.
// A sent-but-unverified attempt never becomes a verified insertion implicitly.
func (c *Core) ReportDelivery(receipt DeliveryReceipt) (DeliveryOutcome, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if receipt.Token == 0 || c.closed || c.currentID != receipt.Token || c.completedToken != receipt.Token ||
		receipt.AttemptID == 0 || c.deliveryAttempt != receipt.AttemptID {
		return 0, ErrStaleTask
	}
	if receipt.Status < DeliveryNotSent || receipt.Status > DeliveryUnknown {
		return 0, ErrInvalidTask
	}
	if c.deliveryStatus != 0 && c.deliveryStatus != receipt.Status {
		return 0, ErrDeliveryConflict
	}
	c.deliveryStatus = receipt.Status
	switch receipt.Status {
	case DeliveryNotSent:
		return DeliveryNeedsPreview, nil
	case DeliveryVerified:
		return DeliveryCompleted, nil
	default:
		return DeliveryUnconfirmed, nil
	}
}
