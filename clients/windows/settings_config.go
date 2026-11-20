package main

import (
	"errors"
	"strconv"
	"strings"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/config"
)

// Recording duration is user-controlled on both hosts. It is applied on the
// next application launch, like other Windows settings read by the host loop.
func uxRecordingSeconds(value string) (int, error) {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds < coreconfig.RecordSecondsMin || seconds > coreconfig.RecordSecondsMax {
		return 0, errors.New("最长录音必须是 1–180 秒之间的整数")
	}
	return seconds, nil
}

func uxUpdatedConfig(old Config, values [6]string) (Config, error) {
	endpoints, err := coreconfig.CanonicalEndpoints(values[0], values[1], values[3], values[4])
	if err != nil {
		return old, err
	}
	// Windows stores the trimmed field, so the shared byte check sees that stored value.
	asrKey := strings.TrimSpace(values[2])
	llmKey := strings.TrimSpace(values[5])
	if coreconfig.StoredCredentialTooLong(asrKey) || coreconfig.StoredCredentialTooLong(llmKey) {
		return old, errors.New(coreconfig.CredentialTooLong)
	}
	cfg := old
	cfg.ASRBaseURL = endpoints.ASRBaseURL
	cfg.ASRModel = endpoints.ASRModel
	cfg.ASRAPIKey = asrKey
	cfg.LLMBaseURL = endpoints.LLMBaseURL
	cfg.TextModel = endpoints.TextModel
	cfg.LLMAPIKey = llmKey
	return cfg, nil
}
