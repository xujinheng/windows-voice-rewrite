package meeting

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Preferences struct {
	FontSize     int32       `json:"font_size"`
	Panes        PaneWeights `json:"panes"`
	CaptionsOnly bool        `json:"captions_only"`
	X            int32       `json:"x"`
	Y            int32       `json:"y"`
	Width        int32       `json:"width"`
	Height       int32       `json:"height"`
}

type CloseChoice int

const (
	CloseLocal CloseChoice = iota
	CloseOwner
)

// ChooseClose quits a demo window. A live meeting asks its owner to stop.
func ChooseClose(hasOwner bool) CloseChoice {
	if hasOwner {
		return CloseOwner
	}
	return CloseLocal
}

func LoadPreferences(path string) (Preferences, error) {
	p := Preferences{FontSize: 16, Panes: DefaultPaneWeights}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(data, &p); err != nil {
		return Preferences{FontSize: 16, Panes: DefaultPaneWeights}, err
	}
	p.FontSize = max(12, min(28, p.FontSize))
	p.Panes = ValidPaneWeights(p.Panes)
	return p, nil
}

func SavePreferences(path string, p Preferences) error {
	p.FontSize = max(12, min(28, p.FontSize))
	p.Panes = ValidPaneWeights(p.Panes)
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
