package voice

import "testing"

func TestDeliveryPolicySharedAcrossNativeClients(t *testing.T) {
	cases := []struct {
		route             string
		target, selection bool
		want              DeliveryAction
	}{
		{"insert", true, true, Insert},
		{"insert", true, false, Insert},
		{"insert", false, true, Preview},
		{"replace", true, true, Replace},
		{"replace", true, false, Preview},
		{"replace", false, true, Preview},
		{"preview", true, true, Preview},
		{"", true, true, Preview},
	}
	for _, tc := range cases {
		if got := Delivery(tc.route, tc.target, tc.selection); got != tc.want {
			t.Errorf("route=%q target=%t selection=%t: got %v want %v", tc.route, tc.target, tc.selection, got, tc.want)
		}
	}
}
