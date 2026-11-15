package main

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"voicerewrite/internal/core/serviceprobe"
)

func uxValidURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Fragment == "" && u.RawQuery == ""
}

func uxUpdatedConfig(old Config, values [6]string) (Config, error) {
	cfg := old
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}
	if !uxValidURL(values[0]) || !uxValidURL(values[3]) {
		return cfg, errors.New("请输入有效的 http:// 或 https:// 服务地址")
	}
	if len(values[0]) > 500 || len(values[3]) > 500 || len(values[1]) > 150 || len(values[4]) > 150 || len(values[2]) > 4000 || len(values[5]) > 4000 {
		return cfg, errors.New("输入内容过长")
	}
	if values[1] == "" {
		return cfg, errors.New("ASR 模型名称不能留空")
	}
	if values[4] == "" {
		values[4] = "auto"
	}
	cfg.ASRBaseURL = strings.TrimRight(values[0], "/")
	cfg.ASRModel = values[1]
	cfg.ASRAPIKey = values[2]
	cfg.LLMBaseURL = strings.TrimRight(values[3], "/")
	cfg.TextModel = values[4]
	cfg.LLMAPIKey = values[5]
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

func uxProbeASRAudio(ctx context.Context, base, apiKey, model string) string {
    return serviceprobe.ProbeASRAudio(ctx, base, apiKey, model)
}
