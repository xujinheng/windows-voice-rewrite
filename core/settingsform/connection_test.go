package settingsform

import (
	"context"
	"encoding/json"
	"testing"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/contract"
)

func TestConnectionSettingsUseSharedCatalog(t *testing.T) {
	form := ConnectionForm()
	if len(form.Pages) != 3 || form.Hint != SettingsHint() {
		t.Fatalf("connection form: %+v", form)
	}
	for _, page := range form.Pages[:2] {
		shared := contract.PageControls(page.ID, contract.Both)
		index := 0
		for _, item := range form.Controls {
			if item.Page != page.ID {
				continue
			}
			if index >= len(shared) || item.ID != shared[index].ID || item.Kind != shared[index].Kind {
				t.Fatalf("%s differs from shared catalog: %+v", page.ID, item)
			}
			index++
		}
		if index != len(shared) {
			t.Fatalf("%s has %d controls, want %d", page.ID, index, len(shared))
		}
	}
}

func TestConnectionSaveUsesCoreConfig(t *testing.T) {
	cfg := coreconfig.Defaults()
	cfg.DictationHotkey = "RightCommand"
	raw, _ := json.Marshal(cfg)
	request, _ := json.Marshal(map[string]any{
		"action": "save", "config": json.RawMessage(raw),
		"values": map[string]string{"asr_base_url": " http://localhost:8081/v1/ ", "text_model": "", "max_record_seconds": "90", "dictation_shortcut": "Mouse4"},
	})
	value, err := ConnectionAction(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	var actual coreconfig.Config
	if err := json.Unmarshal([]byte(value.(map[string]string)["config"]), &actual); err != nil {
		t.Fatal(err)
	}
	if actual.ASRBaseURL != "http://localhost:8081/v1" || actual.TextModel != "auto" || actual.DictationHotkey != "Mouse4" || actual.MaxRecordSeconds != 90 {
		t.Fatalf("unexpected saved config: %+v", actual)
	}
}

func TestMacOSInputFormShowsOnlySupportedControls(t *testing.T) {
	form := ConnectionForm()
	visible := map[string]bool{}
	for _, field := range form.Controls {
		if field.Page == "input" { visible[field.ID] = true }
	}
	for _, id := range []string{"rewrite_shortcut", "dictation_shortcut", "capture_rewrite", "capture_dictation", "max_record_seconds", "input_note_macos"} {
		if !visible[id] { t.Fatalf("missing supported setting %s", id) }
	}
	for _, id := range []string{"microphone", "record_mode", "meeting_asr_mode", "exit_shortcut"} {
		if visible[id] { t.Fatalf("unsupported native field %s", id) }
	}
}
