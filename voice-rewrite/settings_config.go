package main

import (
	"errors"
	"strings"

	coreconfig "voicerewrite/internal/core/config"
)

func uxUpdatedConfig(old Config, values [6]string) (Config, error) {
	endpoints, err := coreconfig.CanonicalEndpoints(values[0], values[1], values[3], values[4])
	if err != nil {
		return old, err
	}
	asrKey := strings.TrimSpace(values[2])
	llmKey := strings.TrimSpace(values[5])
	if len(asrKey) > coreconfig.MaxCredentialBytes || len(llmKey) > coreconfig.MaxCredentialBytes {
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
