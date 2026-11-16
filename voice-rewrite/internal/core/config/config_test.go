package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseMarshalDefaults(t *testing.T) {
	cfg, err := Parse([]byte("{\"meeting_background\":\"team\",\"record_mode\":\"toggle\"}"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MeetingBackground != "team" || cfg.RecordMode != "toggle" || cfg.AudioDeviceID != -1 {
		t.Fatalf("defaults not retained: %+v", cfg)
	}
	data, err := Marshal(cfg)
	if err != nil || !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("marshal: %v", err)
	}
	got, err := Parse(data)
	if err != nil || !reflect.DeepEqual(got, cfg) {
		t.Fatalf("roundtrip: %v", err)
	}
	if _, err := Parse([]byte("{\"record_mode\":")); err == nil {
		t.Fatal("corrupt JSON accepted")
	}
}

func TestNormalizeConfig(t *testing.T) {
	cfg := Defaults()
	cfg.ASRModel = ""
	cfg.HotkeyRecord = ""
	cfg.DictationHotkey = ""
	cfg.RecordMode = ""
	cfg.HotkeyExit = ""
	cfg.MaxResponseTokens = 50
	cfg.MaxRecordSeconds = 181
	cfg.MaxSelectedRunes = -1
	cfg.SelectionCopyAttempts = 0
	cfg.SelectionCopyTimeoutMS = 10
	cfg.ASRAPIKey = "opaque"
	got, err := Normalize(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := Defaults()
	if got.ASRModel != want.ASRModel || got.HotkeyRecord != want.HotkeyRecord || got.DictationHotkey != want.DictationHotkey || got.RecordMode != want.RecordMode || got.HotkeyExit != want.HotkeyExit {
		t.Fatalf("missing defaults: %+v", got)
	}
	if got.MaxResponseTokens != want.MaxResponseTokens || got.MaxRecordSeconds != DefaultRecordSeconds || got.MaxSelectedRunes != DefaultMaxSelectedRunes || got.SelectionCopyAttempts != 3 || got.SelectionCopyTimeoutMS != 1800 {
		t.Fatalf("invalid bounds not clamped: %+v", got)
	}
	if got.ASRAPIKey != "opaque" || cfg.ASRAPIKey != "opaque" {
		t.Fatal("normalization must not handle secrets")
	}
	endpoints, err := CanonicalEndpoints(" http://127.0.0.1:8081/v1/ ", " qwen3-asr ", "http://127.0.0.1:1234/v1", "")
	if err != nil || endpoints.ASRBaseURL != "http://127.0.0.1:8081/v1" || endpoints.ASRModel != "qwen3-asr" || endpoints.TextModel != Defaults().TextModel {
		t.Fatalf("endpoints %+v %v", endpoints, err)
	}
	if _, err := CanonicalEndpoints("http://localhost/v1?key=1", "m", "http://127.0.0.1:1234/v1", "auto"); err == nil {
		t.Fatal("query URL accepted")
	}
	spaced := Defaults()
	spaced.ASRBaseURL = "  http://127.0.0.1:8080/v1  "
	spaced.LLMBaseURL = "\thttp://127.0.0.1:1234/v1\n"
	spaced.MaxRecordSeconds = RecordSecondsMax
	trimmed, err := Normalize(spaced)
	if err != nil || trimmed.ASRBaseURL != "http://127.0.0.1:8080/v1" || trimmed.LLMBaseURL != "http://127.0.0.1:1234/v1" || trimmed.MaxRecordSeconds != RecordSecondsMax {
		t.Fatalf("canonical settings: %+v %v", trimmed, err)
	}
}

func TestNormalizeRejectsInvalidConfiguration(t *testing.T) {
	cases := []struct {
		name, want string
		change     func(*Config)
	}{
		{"meeting mode", "meeting_asr_mode", func(c *Config) { c.MeetingASRMode = "unknown" }},
		{"record mode", "record_mode", func(c *Config) { c.RecordMode = "invalid" }},
		{"audio device", "录音设备编号", func(c *Config) { c.AudioDeviceID = -2 }},
		{"audio device high", "录音设备编号", func(c *Config) { c.AudioDeviceID = 256 }},
		{"ASR URL", "无效的 API 地址", func(c *Config) { c.ASRBaseURL = "file:///tmp/x" }},
		{"URL with credentials", "无效的 API 地址", func(c *Config) { c.ASRBaseURL = "http://a:pass@127.0.0.1:8080/v1" }},
		{"URL with fragment", "无效的 API 地址", func(c *Config) { c.ASRBaseURL = "http://localhost/v1#fragment" }},
		{"LLM URL", "无效的 API 地址", func(c *Config) { c.LLMBaseURL = "https://example.invalid/v1?key=leak" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Defaults()
			tc.change(&cfg)
			_, err := Normalize(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestTransformSecrets(t *testing.T) {
	cfg := Defaults()
	cfg.ASRAPIKey, cfg.LLMAPIKey = "one", "two"
	calls := 0
	got, err := TransformSecrets(cfg, func(s string) (string, error) {
		calls++
		return "sealed:" + s, nil
	})
	if err != nil || calls != 2 || got.ASRAPIKey != "sealed:one" || got.LLMAPIKey != "sealed:two" || cfg.ASRAPIKey != "one" {
		t.Fatalf("transform: %+v, %v", got, err)
	}
	sentinel := errors.New("credential provider failed")
	_, err = TransformSecrets(cfg, func(s string) (string, error) {
		if s == "two" {
			return "", sentinel
		}
		return s, nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("lost native error: %v", err)
	}
}
