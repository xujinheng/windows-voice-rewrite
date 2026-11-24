// Package serviceprobe owns cross-platform provider connection probes.
// ASR uploads generated silence through the real transcription endpoint;
// LLM runs the real rewrite endpoint with a fixed, non-user test sentence.
package serviceprobe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/internal/speech"
	"github.com/xujinheng/windows-voice-rewrite/core/provider"
	"github.com/xujinheng/windows-voice-rewrite/core/textedit"
)

const (
	// StatusIdle is the settings line before a connection check.
	StatusIdle = "尚未检测连接"
	// StatusRunning is the settings line while a check is in flight.
	StatusRunning = "正在检测服务…"
	// StatusStale is the settings line after the address or model changes.
	StatusStale = "连接配置已修改 · 请重新检测"
)

// A service health response and a compatible model are two separate facts.
// Only synthetic test input is sent; no microphone audio or user text.
func ProbeModel(ctx context.Context, kind, base, apiKey, configuredModel string) string {
	if kind == "ASR" {
		return ProbeASRAudio(ctx, base, apiKey, configuredModel)
	}
	if coreconfig.ValidateBaseURL(base) != nil {
		return kind + "：服务地址无效"
	}
	if kind == "LLM" {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cfg := coreconfig.Defaults()
		cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.TextModel = strings.TrimSpace(base), apiKey, configuredModel
		_, err := textedit.Rewrite(ctx, cfg, "hello", "Convert the text to uppercase.")
		if err == nil {
			return "LLM：连接成功，文本模型实际改写测试通过"
		}
		var status *provider.HTTPStatusError
		if errors.As(err, &status) {
			if status.Status == http.StatusUnauthorized || status.Status == http.StatusForbidden {
				return "LLM：API Key 无效或没有权限"
			}
			return fmt.Sprintf("LLM：模型请求失败（HTTP %d）", status.Status)
		}
		if ctx.Err() != nil {
			return "LLM：文本模型测试超时或已取消"
		}
		return "LLM：文本模型测试失败；请检查 API 地址、模型 ID 及服务日志"
	}
	return "未知服务类型"
}

// ProbeASRAudio exercises the actual provider.Endpoint and request format used by
// VoiceRewrite. A models-list success cannot establish support for uploading
// multipart WAV, as some text-only OpenAI-compatible APIs demonstrate.
// The probe sends only locally generated silence, never microphone recordings.
func ProbeASRAudio(ctx context.Context, base, apiKey, model string) string {
	const prefix = "ASR："
	if coreconfig.ValidateBaseURL(base) != nil || strings.TrimSpace(model) == "" {
		return prefix + "请填写有效的服务地址和模型 ID"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cfg := coreconfig.Config{ASRBaseURL: strings.TrimRight(strings.TrimSpace(base), "/"), ASRModel: strings.TrimSpace(model), ASRAPIKey: apiKey}
	// Exactly one second of 16-kHz 16-bit mono silence. Valid WAV and multipart
	// headers, but contains no actual voice or private information.
	req, err := speech.NewASRRequest(ctx, cfg, make([]byte, 16000*2))
	if err != nil {
		return prefix + "无法生成测试音频请求"
	}
	res, err := provider.RequestClient(15 * time.Second).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return prefix + "音频接口检测超时；请检查模型是否加载完成"
		}
		return prefix + "无法连接音频接口；请检查服务地址及网络"
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted:
		return prefix + "音频转写接口接受 WAV 上传（静音测试成功；请试录真实语音）"
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return fmt.Sprintf("%s音频接口收到请求但拒绝测试音频（HTTP %d）；请试录真实语音", prefix, res.StatusCode)
	case http.StatusUnsupportedMediaType:
		return prefix + "HTTP 415：服务不接受当前 WAV 音频上传格式，无法直接用于听写"
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return fmt.Sprintf("%s服务不支持 POST /v1/audio/transcriptions（HTTP %d）", prefix, res.StatusCode)
	case http.StatusUnauthorized, http.StatusForbidden:
		return prefix + "认证失败：API Key 无效或无访问权限"
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return fmt.Sprintf("%s模型服务尚未就绪（HTTP %d）", prefix, res.StatusCode)
	default:
		return fmt.Sprintf("%s音频接口返回 HTTP %d；请检查服务器日志", prefix, res.StatusCode)
	}
}
