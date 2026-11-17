package contractgen

import (
	"go/parser"
	"go/token"
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
	for _, piece := range []string{
		"测试连接", "asr_base_url", "nav_speech", "nav_writing", "nav_input",
		"capture_rewrite", "capture_dictation", "meeting_asr_mode",
		"Text(model.settingsStatus)", "onDisappear { stopCapture() }",
	} {
		if !strings.Contains(settings, piece) {
			t.Fatalf("generated settings missing %s", piece)
		}
	}
	// A SettingsView embedded inside the workspace must never close the
	// entire application window; its host supplies an explicit tab action.
	for _, piece := range []string{
		"@Environment(\\.dismiss) private var dismiss",
		"var onClose: (() -> Void)? = nil",
		"if let onClose { onClose() } else { dismiss() }",
	} {
		if !strings.Contains(settings, piece) {
			t.Fatalf("generated settings lacks safe close contract %q", piece)
		}
	}
	if strings.Contains(settings, "NSApp.keyWindow?.close()") {
		t.Fatal("settings Close must not close the workspace window")
	}
	if strings.Contains(settings, "text_edit_shortcut") || strings.Contains(settings, "TextField(\"文字指令\"") {
		t.Fatal("third global hotkey settings entry must be absent")
	}
	history := first[filepath.Join(filepath.Dir(root), "macos/VoiceRewrite/App/SessionHistory.swift")]
	if !strings.Contains(history, "static func accept") || !strings.Contains(history, "语音结果") || strings.Contains(history, "trimmedTitle") {
		t.Fatal("history policy was not transcribed from Go")
	}
	limits := first[filepath.Join(filepath.Dir(root), "macos/Modules/Storage/Sources/VoiceRewriteStorage/ContractLimits.swift")]
	if !strings.Contains(limits, "func canonicalEndpoints") || !strings.Contains(limits, "func storedCredentialTooLong") || !strings.Contains(limits, "trimmed.contains(\"://\")") || !strings.Contains(limits, "func baseURLEscapesOK") || !strings.Contains(limits, "\"%25\"") {
		t.Fatal("endpoint rules were not generated from Go")
	}
	vectors := first[filepath.Join(filepath.Dir(root), "macos/Modules/Storage/Tests/VoiceRewriteStorageTests/CanonicalEndpointVectors.swift")]
	if !strings.Contains(vectors, "XCTAssertNil") || !strings.Contains(vectors, "count: 4001") || !strings.Contains(vectors, "http://localhost/v1%") || !strings.Contains(vectors, "invalid%ZZ") || !strings.Contains(vectors, "ex%41mple.com") {
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
	if _, err := renderControl(contract.Control{ID: "unmapped", Kind: contract.Choice, Label: "未知设备"}, bindings, actions, status); err == nil {
		t.Fatal("unmapped choice rendered")
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

func TestGeneratorRejectsAnUntranslatedBranch(t *testing.T) {
	historySrc := `package history
import "strings"
func Accept(title, value string) (string, bool) {
	if strings.HasPrefix(value, "x") { return "", false }
	if strings.TrimSpace(value) == "" || len(value) > MaxBytes { return "", false }
	if strings.TrimSpace(title) == "" { return "语音结果", true }
	return title, true
}
func KeepCount(texts []string) int {
	bytes := 0
	keep := 0
	for _, text := range texts {
		if strings.Contains(text, "x") { return keep }
		if keep >= Limit || bytes+len(text) > MaxBytes { return keep }
		bytes += len(text)
		keep++
	}
	return keep
}`
	file, err := parser.ParseFile(token.NewFileSet(), "history.go", historySrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := acceptPolicy(findFunc(file, "Accept")); err == nil {
		t.Fatal("HasPrefix was translated")
	}
	plain := `package history
func Accept(title, value string) (string, bool) {
	if value == "x" { return "", false }
	if strings.TrimSpace(value) == "" || len(value) > MaxBytes { return "", false }
	if strings.TrimSpace(title) == "" { return "语音结果", true }
	return title, true
}`
	plainFile, err := parser.ParseFile(token.NewFileSet(), "history.go", plain, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := acceptPolicy(findFunc(plainFile, "Accept")); err == nil {
		t.Fatal("extra rejection was ignored")
	}
	if _, _, err := keepPolicy(findFunc(file, "KeepCount")); err == nil {
		t.Fatal("Contains was translated")
	}
	escapeSrc := `package config
import "strings"
func BaseURLEscapesOK(raw string) bool {
	if strings.HasPrefix(raw, "http://localhost/v1%") { return false }
	return percentHexOK(raw, false)
}
func percentHexOK(s string, host bool) bool { return true }
func baseURLHost(raw string) string { return raw }
func hexNibble(c byte) int { return 0 }`
	escapeFile, err := parser.ParseFile(token.NewFileSet(), "config.go", escapeSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireEscapeClosure(escapeFile); err == nil {
		t.Fatal("HasPrefix escape branch was ignored")
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
