package main

import (
	meetingcore "voicerewrite/internal/core/meeting"
	"os"
	"path/filepath"
	"testing"
)

func TestMeetingPreferencesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "meeting-ui.json")
	want := meetingPreferences{FontSize: 22, Panes: meetingcore.PaneWeights{110, 40, 80, 50, 300, 30}, CaptionsOnly: true, X: -900, Y: 30, Width: 640, Height: 720}
	if err := saveMeetingPreferences(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadMeetingPreferences(path)
	if err != nil || got != want {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
	want.FontSize = 18
	if err := saveMeetingPreferences(path, want); err != nil {
		t.Fatal(err)
	}
	got, err = loadMeetingPreferences(path)
	if err != nil || got != want {
		t.Fatalf("replacement got %+v, %v", got, err)
	}
}

func TestMeetingPreferencesDefaultsAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meeting-ui.json")
	got, err := loadMeetingPreferences(path)
	if err != nil || got.FontSize != 16 || got.Panes != meetingcore.DefaultPaneWeights {
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
		got, err = loadMeetingPreferences(path)
		if err != nil || got.FontSize != tc.font || got.Panes != meetingcore.DefaultPaneWeights {
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
		got, err := loadMeetingPreferences(path)
		if err != nil || got.Panes != meetingcore.DefaultPaneWeights {
			t.Fatalf("invalid pane sizing escaped validation: %+v %v", got, err)
		}
	}
	if err := os.WriteFile(path, []byte(`{"font_size":`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = loadMeetingPreferences(path)
	if err == nil || got.FontSize != 16 {
		t.Fatalf("corrupt preferences: %+v %v", got, err)
	}
}

func TestMeetingPreferencesClampRemovedMonitor(t *testing.T) {
	p := meetingPreferences{X: -2000, Y: 1800, Width: 1800, Height: 1400}
	x, y, w, h := p.geometry(desktopWorkArea{left: 0, top: 30, right: 1024, bottom: 768}, 1)
	if x != 0 || y != 30 || w != 1024 || h != 738 {
		t.Fatalf("offscreen geometry = %d,%d %dx%d", x, y, w, h)
	}
	p = meetingPreferences{X: -1200, Y: 50, Width: 700, Height: 750}
	x, y, w, h = p.geometry(desktopWorkArea{left: -1280, top: 0, right: 0, bottom: 1000}, 1)
	if x != -1200 || y != 50 || w != 700 || h != 750 {
		t.Fatalf("valid negative geometry = %d,%d %dx%d", x, y, w, h)
	}
	p = meetingPreferences{Width: -4, Height: 2}
	_, _, w, h = p.geometry(desktopWorkArea{right: 1920, bottom: 1080}, 1)
	if w != 640 || h != 720 {
		t.Fatalf("invalid size = %dx%d", w, h)
	}
}
