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
	values := map[string]string{"asr_base_url": " http://localhost:8081/v1/ ", "text_model": "", "max_record_seconds": "90", "dictation_shortcut": "Mouse4"}
	request, _ := json.Marshal(map[string]any{"action": "save", "config": json.RawMessage(raw), "values": values})
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
	// Windows uses the typed Core update; macOS uses the JSON ABI.
	typed, err := Update(cfg, values)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := coreconfig.Marshal(typed)
	if string(canonical) != value.(map[string]string)["config"] {
		t.Fatal("native and JSON settings disagree")
	}
}

func TestMacOSInputFormShowsOnlySupportedControls(t *testing.T) {
	form := ConnectionForm()
	visible := map[string]bool{}
	for _, field := range form.Controls {
		if field.Page == "input" {
			visible[field.ID] = true
		}
	}
	for _, id := range []string{"rewrite_shortcut", "dictation_shortcut", "capture_rewrite", "capture_dictation", "max_record_seconds", "input_note_macos"} {
		if !visible[id] {
			t.Fatalf("missing supported setting %s", id)
		}
	}
	for _, id := range []string{"microphone", "record_mode", "meeting_asr_mode", "exit_shortcut"} {
		if visible[id] {
			t.Fatalf("unsupported native field %s", id)
		}
	}
}

func TestDeployDirectoryAndEndpointSaveIndependently(t *testing.T) {
	cfg := coreconfig.Defaults()
	for _, values := range []map[string]string{
		{"asr_deploy_dir": "/example/models"},
		{"asr_base_url": "http://127.0.0.1:9876/v1"},
	} {
		raw, _ := json.Marshal(cfg)
		req, _ := json.Marshal(map[string]any{
			"action": "save", "config": json.RawMessage(raw), "values": values,
		})
		result, err := ConnectionAction(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(result.(map[string]string)["config"]), &cfg); err != nil {
			t.Fatal(err)
		}
	}
	if cfg.ASRDeployDir != "/example/models" || cfg.ASRBaseURL != "http://127.0.0.1:9876/v1" {
		t.Fatalf("saving endpoint overwrote selected Deploy directory: %+v", cfg)
	}
}

func TestPortableShortcutCollisionRejectsBothSettingsEntryPoints(t *testing.T) {
	cfg := coreconfig.Defaults()
	values := map[string]string{"dictation_shortcut": "Mouse5"}
	if _, err := Update(cfg, values); err == nil {
		t.Fatal("typed client accepted colliding shortcuts")
	}
	raw, _ := json.Marshal(cfg)
	request, _ := json.Marshal(map[string]any{"action": "save", "config": json.RawMessage(raw), "values": values})
	if _, err := ConnectionAction(context.Background(), request); err == nil {
		t.Fatal("JSON ABI accepted shortcuts rejected by Windows")
	}
}
