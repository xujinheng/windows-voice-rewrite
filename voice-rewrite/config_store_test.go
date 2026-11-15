package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigStoreCreatesDefaultsAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	got, err := loadConfigFrom(path)
	if err != nil || got != defaults() {
		t.Fatalf("initial load: %+v, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("configuration not created: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("configuration is not private: %v", info.Mode())
	}
	got.MeetingBackground = "retained"
	got.ASRAPIKey = "test-only-not-a-real-key"
	got.LLMAPIKey = "another-test-value"
	if err := saveConfigTo(path, got); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		data, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(data), got.ASRAPIKey) || strings.Contains(string(data), got.LLMAPIKey) || !strings.Contains(string(data), "dpapi:v1:") {
			t.Fatalf("API keys not DPAPI-protected: %v", err)
		}
	}
	reloaded, err := loadConfigFrom(path)
	if err != nil || reloaded != got {
		t.Fatalf("roundtrip: %+v, %v", reloaded, err)
	}
}

func TestConfigStoreRejectsCorruptAndInvalidData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, tc := range []struct{ name, data, want string }{
		{"truncated JSON", "{\"record_mode\":", "配置文件 JSON 错误"},
		{"bad mode", "{\"record_mode\":\"invalid\"}", "record_mode"},
		{"bad URL", "{\"asr_base_url\":\"ftp://invalid\"}", "无效的 API 地址"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := loadConfigFrom(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestConfigStoreSaveFailureKeepsPreviousFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveConfigTo(path, defaults()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaults()
	cfg.RecordMode = "invalid"
	if err := saveConfigTo(path, cfg); err == nil {
		t.Fatal("invalid configuration saved")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("invalid save overwrote configuration: %v", err)
	}
	blocked := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveConfigTo(filepath.Join(blocked, "config.json"), defaults()); err == nil {
		t.Fatal("filesystem failure was ignored")
	}
}

func TestConfigStoreRenameFailureCleansTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveConfigTo(path, defaults()); err == nil {
		t.Fatal("expected atomic replacement to fail on a directory")
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".config-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files remain: %v (%v)", matches, err)
	}
}
