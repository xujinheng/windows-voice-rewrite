package voice

import "testing"

func TestPortableDeliveryDecision(t *testing.T) {
	for _, tc := range []struct {
		name, route      string
		target, selected, readOnly bool
		want                       DeliveryAction
	}{
		{"insert", RouteInsert, true, false, false, DeliveryInsert},
		{"focus lost", RouteInsert, false, false, false, DeliveryPreview},
		{"replace", RouteReplace, true, true, false, DeliveryReplace},
		{"terminal selection cannot be replaced", RouteReplace, true, true, true, DeliveryPreview},
		{"terminal selection cannot be applied later", RouteReplace, false, true, true, DeliveryPreview},
		{"terminal selection cannot receive insert", RouteInsert, true, true, true, DeliveryPreview},
		{"selection absent", RouteReplace, true, false, false, DeliveryPreview},
		{"selection and target absent", RouteReplace, false, false, false, DeliveryPreview},
		{"target lost, manual apply available", RouteReplace, false, true, false, DeliveryPreviewApply},
		{"explain", RoutePreview, true, true, false, DeliveryPreview},
		{"unknown", "invalid", true, true, false, DeliveryPreview},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideDelivery(tc.route, DeliveryFacts{TargetCurrent: tc.target, SelectionCaptured: tc.selected, SelectionReadOnly: tc.readOnly})
			if got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}
