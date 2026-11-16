package contractgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
