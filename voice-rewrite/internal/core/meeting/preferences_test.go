package meeting

import (
	"testing"
	"os"
	"path/filepath"
)

func TestMeetingPreferencesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "meeting-ui.json")
	want := Preferences{FontSize: 22, Panes: PaneWeights{110, 40, 80, 50, 300, 30}, CaptionsOnly: true, X: -900, Y: 30, Width: 640, Height: 720}
	if err := SavePreferences(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPreferences(path)
	if err != nil || got != want {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
	want.FontSize = 18
	if err := SavePreferences(path, want); err != nil {
		t.Fatal(err)
	}
	got, err = LoadPreferences(path)
	if err != nil || got != want {
		t.Fatalf("replacement got %+v, %v", got, err)
	}
}

func TestMeetingPreferencesDefaultsAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meeting-ui.json")
	got, err := LoadPreferences(path)
	if err != nil || got.FontSize != 16 || got.Panes != DefaultPaneWeights {
		t.Fatalf("missing preferences: %+v %v", got, err)
	}
	for _, tc := range []struct {
		body string
		font int32
	}{
		{`{"font_size":3}`, 12}, {`{"font_size":99}`, 28}, {`{}`, 16},
	} {
		if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		got, err = LoadPreferences(path)
		if err != nil || got.FontSize != tc.font || got.Panes != DefaultPaneWeights {
			t.Fatalf("%s: %+v %v", tc.body, got, err)
		}
	}
	for _, invalid := range []string{
		`{"panes":[0,0,0,0,0,0]}`,
		`{"panes":[100,80,60,-5,100,30]}`,
		`{"panes":[100,80,60,200000,100,30]}`,
	} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadPreferences(path)
		if err != nil || got.Panes != DefaultPaneWeights {
			t.Fatalf("invalid pane sizing escaped validation: %+v %v", got, err)
		}
	}
	if err := os.WriteFile(path, []byte(`{"font_size":`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadPreferences(path)
	if err == nil || got.FontSize != 16 {
		t.Fatalf("corrupt preferences: %+v %v", got, err)
	}
}

