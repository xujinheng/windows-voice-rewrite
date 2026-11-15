// Package serviceprobe owns cross-platform provider connection probes.
// ASR uploads generated silence through the real transcription endpoint;
// LLM checks the model discovery endpoint without sending user content.
package serviceprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/provider"
	"voicerewrite/internal/core/speech"
)

func validURL(value string) bool { return coreconfig.ValidateBaseURL(value) == nil }

// A service health response and a compatible model are two separate facts.
// This probe never uploads microphone audio or sends text prompts.
func ProbeModel(ctx context.Context, kind, base, apiKey, configuredModel string) string {
	if kind == "ASR" && strings.TrimSpace(configuredModel) != "" {
		return ProbeASRAudio(ctx, base, apiKey, configuredModel)
	}
	if !validURL(base) {
		return kind + "：服务地址无效"
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(base), "/"))
	if err != nil {
		return kind + "：服务地址无效"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return kind + "：检测请求无效"
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	res, err := provider.RequestClient(4 * time.Second).Do(req)
	if err != nil {
		return kind + "：无法连接，请检查 API 地址及网络"
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
		var response struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if e := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&response); e != nil {
			return kind + "：API 可达 · HTTP 200，但模型列表格式无法识别；请试用实际功能"
		}
		if len(response.Data) == 0 {
			return kind + "：API 可达 · HTTP 200，模型列表为空；请检查是否已加载模型"
		}
		chosen := strings.TrimSpace(configuredModel)
		if chosen != "" && chosen != "auto" {
			for _, model := range response.Data {
				if model.ID == chosen {
					return kind + "：模型 " + chosen + " 已在服务中发现（未验证实际任务）"
				}
			}
			return kind + "：服务可达，但模型 " + chosen + " 未列出；请核对 ID 或服务别名"
		}
		return fmt.Sprintf("%s：已连接 · 可发现 %d 个模型（未验证实际任务）", kind, len(response.Data))
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return kind + "：服务可达；此接口不提供模型列表，请实际试用"
	case http.StatusUnauthorized, http.StatusForbidden:
		return kind + "：API Key 无效或没有权限"
	case http.StatusServiceUnavailable:
		return kind + "：服务暂时不可用（503）"
	default:
		return fmt.Sprintf("%s：API 返回 HTTP %d", kind, res.StatusCode)
	}
}

// ProbeASRAudio exercises the actual provider.Endpoint and request format used by
// VoiceRewrite. A models-list success cannot establish support for uploading
// multipart WAV, as some text-only OpenAI-compatible APIs demonstrate.
// The probe sends only locally generated silence, never microphone recordings.
func ProbeASRAudio(ctx context.Context, base, apiKey, model string) string {
	const prefix = "ASR："
	if !validURL(base) || strings.TrimSpace(model) == "" {
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
