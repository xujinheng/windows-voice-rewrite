package main

import (
	meetingcore "voicerewrite/internal/core/meeting"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type meetingPreferences struct {
	FontSize     int32              `json:"font_size"`
	Panes        meetingcore.PaneWeights `json:"panes"`
	CaptionsOnly bool               `json:"captions_only"`
	X            int32              `json:"x"`
	Y            int32              `json:"y"`
	Width        int32              `json:"width"`
	Height       int32              `json:"height"`
}

func loadMeetingPreferences(path string) (meetingPreferences, error) {
	p := meetingPreferences{FontSize: 16, Panes: meetingcore.DefaultPaneWeights}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(data, &p); err != nil {
		return meetingPreferences{FontSize: 16, Panes: meetingcore.DefaultPaneWeights}, err
	}
	p.FontSize = max(12, min(28, p.FontSize))
	p.Panes = meetingcore.ValidPaneWeights(p.Panes)
	return p, nil
}

func saveMeetingPreferences(path string, p meetingPreferences) error {
	p.FontSize = max(12, min(28, p.FontSize))
	p.Panes = meetingcore.ValidPaneWeights(p.Panes)
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "meeting-ui-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (p meetingPreferences) geometry(work desktopWorkArea, scale float64) (x, y, w, h int32) {
	w, h = p.Width, p.Height
	if w < int32(560*scale) || h < int32(640*scale) {
		w, h = int32(640*scale), int32(720*scale)
	}
	w, h = desktopFitSize(w, h, work, 0)
	x, y = p.X, p.Y
	if p.Width <= 0 || p.Height <= 0 {
		x, y = work.right-w-12, work.top+12
	}
	x, y = desktopClampPosition(x, y, w, h, work)
	return
}
