package voice

import "testing"

func TestDeliveryDecisionSharedAcrossNativeHosts(t *testing.T) {
	var core Core
	token := core.Apply(Command{Kind: CommandStart}).Token
	defer core.Apply(Command{Kind: CommandShutdown})
	cases := []struct {
		name  string
		route string
		facts DeliveryFacts
		want  DeliveryAction
	}{
		{"dictation", RouteInsert, DeliveryFacts{TargetCurrent: true}, DeliveryInsert},
		{"replacement", RouteReplace, DeliveryFacts{TargetCurrent: true, SelectionAvailable: true}, DeliveryReplace},
		{"answer", RoutePreview, DeliveryFacts{TargetCurrent: true, SelectionAvailable: true}, DeliveryPreview},
		{"focus moved", RouteInsert, DeliveryFacts{}, DeliveryPreview},
		{"selection absent", RouteReplace, DeliveryFacts{TargetCurrent: true}, DeliveryPreview},
		{"selection changed at native delivery", RouteReplace, DeliveryFacts{TargetCurrent: true, SelectionAvailable: true, Failed: true}, DeliveryPreview},
		{"dictation paste failed", RouteInsert, DeliveryFacts{TargetCurrent: true, Failed: true}, DeliveryPreview},
		{"rewrite paste failed", RouteReplace, DeliveryFacts{TargetCurrent: true, SelectionAvailable: true, Failed: true}, DeliveryPreview},
		{"unknown route", "invalid", DeliveryFacts{TargetCurrent: true}, DeliveryPreview},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := core.DecideDelivery(token, Result{Text: "saved result", Route: tc.route}, tc.facts)
			if got != tc.want {
				t.Fatalf("delivery action = %d, want %d", got, tc.want)
			}
		})
	}
	core.Apply(Command{Kind: CommandStart})
	if got := core.DecideDelivery(token, Result{Route: RouteInsert}, DeliveryFacts{TargetCurrent: true}); got != DeliveryDrop {
		t.Fatalf("stale task must not reach native delivery, got %d", got)
	}
}
