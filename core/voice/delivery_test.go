package voice

import "testing"

func TestPortableDeliveryDecision(t *testing.T) {
	for _, tc := range []struct {
		name, route      string
		target, selected bool
		want             DeliveryAction
	}{
		{"insert", RouteInsert, true, false, DeliveryInsert},
		{"focus lost", RouteInsert, false, false, DeliveryPreview},
		{"replace", RouteReplace, true, true, DeliveryReplace},
		{"selection absent", RouteReplace, true, false, DeliveryPreview},
		{"target lost", RouteReplace, false, true, DeliveryPreview},
		{"explain", RoutePreview, true, true, DeliveryPreview},
		{"unknown", "invalid", true, true, DeliveryPreview},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideDelivery(tc.route, DeliveryFacts{TargetCurrent: tc.target, SelectionCaptured: tc.selected})
			if got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}
