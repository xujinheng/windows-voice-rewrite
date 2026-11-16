package main

import (
	"context"
	"errors"
	"strings"

	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/serviceprobe"
)

func uxUpdatedConfig(old Config, values [6]string) (Config, error) {
	endpoints, err := coreconfig.CanonicalEndpoints(values[0], values[1], values[3], values[4])
	if err != nil {
		return old, err
	}
	asrKey := strings.TrimSpace(values[2])
	llmKey := strings.TrimSpace(values[5])
	if len(asrKey) > coreconfig.MaxCredentialBytes || len(llmKey) > coreconfig.MaxCredentialBytes {
		return old, errors.New("输入内容过长")
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

// Windows surfaces the shared Core probe result in native Win32 settings.
func uxProbe(ctx context.Context, kind, base string) string {
	return uxProbeWithKey(ctx, kind, base, "")
}

func uxProbeWithKey(ctx context.Context, kind, base, apiKey string) string {
	return uxProbeModel(ctx, kind, base, apiKey, "")
}

func uxProbeModel(ctx context.Context, kind, base, apiKey, configuredModel string) string {
	return serviceprobe.ProbeModel(ctx, kind, base, apiKey, configuredModel)
}
