package voice

// DeliveryAction is the requested native text effect, not proof of insertion.
type DeliveryAction uint8

const (
	DeliveryPreview DeliveryAction = iota + 1
	DeliveryInsert
	DeliveryReplace
)

// DeliveryFacts are observed by the host. The native write path must recheck
// the editor and selection immediately before any mutation.
type DeliveryFacts struct {
	TargetCurrent     bool
	SelectionCaptured bool
}

// DecideDelivery owns the portable fallback policy for every client.
// Hosts implement the selected native effect but never auto-retry an unknown paste.
func DecideDelivery(route string, facts DeliveryFacts) DeliveryAction {
	if !facts.TargetCurrent {
		return DeliveryPreview
	}
	switch route {
	case RouteInsert:
		return DeliveryInsert
	case RouteReplace:
		if facts.SelectionCaptured {
			return DeliveryReplace
		}
	}
	return DeliveryPreview
}
