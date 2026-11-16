package contractgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"voicerewrite/internal/contract"
)

func TestGenerateHostFilesFromCore(t *testing.T) {
	root := moduleRoot(t)
	first, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("unstable file set %d %d", len(first), len(second))
	}
	for path, content := range first {
		if second[path] != content {
			t.Fatalf("unstable %s", path)
		}
		if !strings.Contains(content, "Source:") || !strings.HasSuffix(content, "\n") {
			t.Fatalf("bad envelope %s", path)
		}
	}
	indicator := first[filepath.Join(filepath.Dir(root), "macos/VoiceRewrite/UI/IndicatorMath.swift")]
	for _, piece := range []string{
		"case recording = 1",
		"static let intro: TimeInterval = 0.170",
		"static func label(_ phase: IndicatorPhase, mode: String) -> String",
		"正在转写…",
		"识别指令…",
		"return errorAccent",
		"timeIntervalSince",
		"UInt8",
	} {
		if !strings.Contains(indicator, piece) {
			t.Fatalf("indicator missing %s", piece)
		}
	}
	settings := first[filepath.Join(filepath.Dir(root), "macos/VoiceRewrite/UI/SettingsView.swift")]
	if !strings.Contains(settings, "测试连接") || !strings.Contains(settings, "asr_base_url") {
		t.Fatal("settings view is not driven by the catalog")
	}
	history := first[filepath.Join(filepath.Dir(root), "macos/VoiceRewrite/App/SessionHistory.swift")]
	if !strings.Contains(history, "static func accept") || !strings.Contains(history, "语音结果") || strings.Contains(history, "trimmedTitle") {
		t.Fatal("history policy was not transcribed from Go")
	}
	limits := first[filepath.Join(filepath.Dir(root), "macos/Modules/Storage/Sources/VoiceRewriteStorage/ContractLimits.swift")]
	if !strings.Contains(limits, "func canonicalEndpoints") || !strings.Contains(limits, "func storedCredentialTooLong") || !strings.Contains(limits, "trimmed.contains(\"://\")") {
		t.Fatal("endpoint rules were not generated from Go")
	}
	vectors := first[filepath.Join(filepath.Dir(root), "macos/Modules/Storage/Tests/VoiceRewriteStorageTests/CanonicalEndpointVectors.swift")]
	if !strings.Contains(vectors, "XCTAssertNil") || !strings.Contains(vectors, "count: 4001") {
		t.Fatal("endpoint vectors")
	}
	bindings, actions, status := settingsMaps()
	for _, control := range contract.Controls() {
		if !control.VisibleOn(contract.MacOS) {
			continue
		}
		line, err := renderControl(control, bindings, actions, status)
		if err != nil || !strings.Contains(line, control.ID) {
			t.Fatalf("visible %s: %v", control.ID, err)
		}
	}
	if _, err := renderControl(contract.Control{ID: "microphone", Kind: contract.Choice, Label: "麦克风"}, bindings, actions, status); err == nil {
		t.Fatal("choice rendered")
	}
	if _, err := renderControl(contract.Control{ID: "extra", Kind: contract.Button, Label: "多余"}, bindings, actions, status); err == nil {
		t.Fatal("empty button rendered")
	}
	logText := first[filepath.Join(filepath.Dir(root), "macos/VoiceRewrite/App/DiagnosticLog.swift")]
	for _, piece := range []string{
		"invalid log rotation policy",
		"log path is a directory:",
		"try manager.moveItem(atPath: path, toPath: oldest)",
	} {
		if !strings.Contains(logText, piece) {
			t.Fatalf("log rotation missing %s", piece)
		}
	}
	holdText := first[filepath.Join(filepath.Dir(root), "macos/Modules/Interaction/Sources/VoiceRewriteInteraction/SideHold.swift")]
	if !strings.Contains(holdText, "0.280") {
		t.Fatal("side hold was not generated from the Go duration")
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod")
		}
		dir = parent
	}
}
