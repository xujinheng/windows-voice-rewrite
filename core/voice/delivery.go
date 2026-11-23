package voice

// DeliveryAction is the requested native text effect, not proof of insertion.
type DeliveryAction uint8

const (
	DeliveryPreview DeliveryAction = iota + 1
	DeliveryInsert
	DeliveryReplace
	DeliveryPreviewApply // Preview retaining the captured selection for explicit Apply.
)

// DeliveryFacts are observed by the host. The native write path must recheck
// the editor and selection immediately before any mutation.
type DeliveryFacts struct {
	TargetCurrent      bool
	SelectionCaptured  bool
	SelectionReadOnly  bool // Native selection is not an editable text range (e.g. terminal scrollback).
}

// DecideDelivery owns the portable fallback policy for every client.
// Hosts implement the selected native effect but never auto-retry an unknown paste.
func DecideDelivery(route string, facts DeliveryFacts) DeliveryAction {
	if facts.SelectionCaptured && facts.SelectionReadOnly {
		return DeliveryPreview
	}
	switch route {
	case RouteInsert:
		if facts.TargetCurrent {
			return DeliveryInsert
		}
	case RouteReplace:
		if facts.SelectionCaptured {
			if facts.TargetCurrent {
				return DeliveryReplace
			}
			return DeliveryPreviewApply
		}
	}
	return DeliveryPreview
}
