package voice

import "testing"

func TestPortableDeliveryDecision(t *testing.T) {
	for _, tc := range []struct {
		name, route string
		target, selected bool
		want DeliveryAction
	}{
		{"insert", RouteInsert, true, false, DeliveryInsert},
		{"focus lost", RouteInsert, false, false, DeliveryPreview},
		{"replace", RouteReplace, true, true, DeliveryReplace},
		{"selection absent", RouteReplace, true, false, DeliveryPreview},
		{"target lost", RouteReplace, false, true, DeliveryPreview},
		{"explain", RoutePreview, true, true, DeliveryPreview},
		{"unknown", "invalid", true, true, DeliveryPreview},
	} {
		for _, host := range []string{"windows", "macos", "cli"} {
			t.Run(host+"/"+tc.name, func(t *testing.T) {
				got := DecideDelivery(Result{Route: tc.route}, DeliveryFacts{TargetCurrent: tc.target, SelectionCaptured: tc.selected})
				if got != tc.want { t.Fatalf("got %d want %d", got, tc.want) }
			})
		}
	}
}
