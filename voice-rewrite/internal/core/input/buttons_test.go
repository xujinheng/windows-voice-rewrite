package input

import "testing"

func TestSideButtonFlow(t *testing.T) {
	for _, tc := range []struct {
		button uint16
		want   string
	}{
		{1, "dictation"}, {2, "rewrite"}, {3, "ignore"}, {0, "ignore"},
	} {
		if got := SideButtonFlow(tc.button, 2, 1); got != tc.want {
			t.Fatalf("%d: %q != %q", tc.button, got, tc.want)
		}
	}
	if got := SideButtonFlow(2, 0, 1); got != "ignore" {
		t.Fatalf("unexpected: %s", got)
	}
}
