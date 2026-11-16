package contract

import (
	"strconv"
	"strings"
	"testing"

	coreconfig "voicerewrite/internal/core/config"
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
	if join(windowsInput) != "rewrite_shortcut dictation_shortcut microphone" {
		t.Fatalf("windows input: %v", windowsInput)
	}
	macInput := ids(Stacked("input", MacOS))
	if join(macInput) != "max_record_seconds rewrite_shortcut dictation_shortcut text_edit_shortcut" {
		t.Fatalf("macos input: %v", macInput)
	}
	save, ok := Find("save")
	probe, pok := Find("probe_asr")
	if !ok || !pok || save.Label != "保存设置" || probe.Label != "测试连接" {
		t.Fatalf("actions: %+v %+v", save, probe)
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
