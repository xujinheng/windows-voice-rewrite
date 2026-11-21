package voice

// DeliveryAction is the portable decision following a successful Core.Execute.
// Native adapters perform the actual editor/clipboard operations.
type DeliveryAction uint8

const (
	DeliveryDrop DeliveryAction = iota
	DeliveryInsert
	DeliveryReplace
	DeliveryPreview
)

// DeliveryFacts are observations from the native editor adapter. SelectionAvailable
// means text was captured, not that the live selection is still identical:
// each OS adapter must recheck its editor immediately before replacement.
// Failed means native delivery was rejected, or a paste could not be confirmed
// as sent; an uncertain paste must never be retried automatically.
type DeliveryFacts struct {
	TargetCurrent    bool
	SelectionAvailable bool
	Failed           bool
}

// DecideDelivery is the single policy for insert, replace and recoverable
// preview. A stale generation is discarded; all other non-deliverable results
// remain recoverable in Core.History. The host never replays an uncertain paste.
func (c *Core) DecideDelivery(token uint64, result Result, facts DeliveryFacts) DeliveryAction {
	if !c.Snapshot(0, token).Valid {
		return DeliveryDrop
	}
	if facts.Failed || !facts.TargetCurrent {
		return DeliveryPreview
	}
	switch result.Route {
	case RouteInsert:
		return DeliveryInsert
	case RouteReplace:
		if facts.SelectionAvailable {
			return DeliveryReplace
		}
	}
	return DeliveryPreview
}
