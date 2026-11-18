package config

import (
	"strings"
	"testing"
)

func TestASRHintModeDefaultsAndConfigRoundTrip(t *testing.T) {
	legacy, err := Parse([]byte(`{"asr_model":"qwen3-asr","meeting_terms":"private meeting"}`))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.ASRHintMode != ASRHintOff || legacy.ASRVocabulary != "" {
		t.Fatalf("legacy settings unexpectedly enable hints: %#v", legacy)
	}
	for _, mode := range []string{ASRHintOff, ASRHintAuto, ASRHintVocabulary} {
		cfg := Defaults()
		cfg.ASRHintMode = mode
		cfg.ASRVocabulary = "macOS = mac O S\nPR = P R"
		raw, err := Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Parse(raw)
		if err != nil || decoded.ASRHintMode != mode || decoded.ASRVocabulary != cfg.ASRVocabulary {
			t.Fatalf("failed round trip for %q: %+v, %v", mode, decoded, err)
		}
	}
}

func TestASRHintModeRejectsInvalidAndTooLargeVocabulary(t *testing.T) {
	cfg := Defaults()
	cfg.ASRHintMode = "force-unknown-provider"
	if _, err := Normalize(cfg); err == nil {
		t.Fatal("unknown mode accepted")
	}
	cfg.ASRHintMode = ASRHintVocabulary
	cfg.ASRVocabulary = strings.Repeat("x", MaxASRVocabularyBytes+1)
	if _, err := Normalize(cfg); err == nil {
		t.Fatal("oversized vocabulary accepted")
	}
	cfg.ASRVocabulary = strings.Repeat("词", MaxASRVocabularyBytes/3)
	if _, err := Normalize(cfg); err != nil {
		t.Fatal(err)
	}
}
