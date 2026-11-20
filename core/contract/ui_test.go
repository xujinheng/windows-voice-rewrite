package contract

import (
	"strconv"
	"strings"
	"testing"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/config"
)

func TestSettingsCatalogKeepsSharedPagesAndOrder(t *testing.T) {
	pages := Pages()
	if len(pages) != 3 || pages[0].ID != "speech" || pages[1].ID != "writing" || pages[2].ID != "input" {
		t.Fatalf("pages: %+v", pages)
	}
	if pages[0].Title != "语音识别" || pages[1].Section != "模型配置" || pages[2].Subtitle == "" {
		t.Fatalf("page copy: %+v", pages)
	}
	speech := ids(Stacked("speech", Windows))
	if join(speech) != "asr_base_url asr_model asr_api_key" {
		t.Fatalf("speech rows: %v", speech)
	}
	if join(ids(Stacked("speech", MacOS))) != join(speech) {
		t.Fatal("speech rows differ between hosts")
	}
	writing := ids(Stacked("writing", Both))
	if join(writing) != "llm_base_url text_model llm_api_key" {
		t.Fatalf("writing rows: %v", writing)
	}
	windowsInput := ids(Stacked("input", Windows))
	if join(windowsInput) != "rewrite_shortcut dictation_shortcut microphone max_record_seconds" {
		t.Fatalf("windows input: %v", windowsInput)
	}
	macInput := ids(Stacked("input", MacOS))
	if join(macInput) != "rewrite_shortcut dictation_shortcut microphone max_record_seconds" {
		t.Fatalf("macos input: %v", macInput)
	}
	for _, control := range Controls() {
		if control.Platform == Both && control.Gap != "" || control.Platform != Both && !strings.HasPrefix(control.Gap, "#") {
			t.Fatalf("gap %s %q", control.ID, control.Gap)
		}
	}
	save, ok := Find("save")
	probe, pok := Find("probe_asr")
	if !ok || !pok || save.Label != "保存设置" || probe.Label != "测试连接" {
		t.Fatalf("actions: %+v %+v", save, probe)
	}
	if _, ok := Find("text_edit_shortcut"); ok {
		t.Fatal("third global shortcut must not appear in settings")
	}
	for _, id := range []string{"record_mode", "exit_shortcut", "capture_exit", "system_prompt", "max_response_tokens", "max_selected_characters"} {
		field, ok := Find(id)
		if !ok || !field.VisibleOn(MacOS) || field.VisibleOn(Windows) {
			t.Fatalf("macOS settings catalog missing %s", id)
		}
	}
	for _, id := range []string{"discover_asr_models", "discover_llm_models"} {
		entry, ok := Find(id)
		if !ok || entry.Kind != Button || !entry.VisibleOn(Windows) || !entry.VisibleOn(MacOS) || entry.Label != "发现模型" || entry.Stacked {
			t.Fatalf("discovery button %s is not a shared non-stacked action: %+v", id, entry)
		}
	}
	if len(PageControls("speech", Windows)) != len(PageControls("speech", MacOS)) {
		t.Fatal("speech page controls diverged")
	}
	note, ok := Find("record_note")
	if !ok || !strings.Contains(note.Label, strconv.Itoa(coreconfig.RecordSecondsMax)) {
		t.Fatalf("record note %q does not state the core maximum %d", note.Label, coreconfig.RecordSecondsMax)
	}
}

func ids(controls []Control) []string {
	out := make([]string, len(controls))
	for i, control := range controls {
		out[i] = control.ID
	}
	return out
}

func join(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += " "
		}
		out += part
	}
	return out
}
