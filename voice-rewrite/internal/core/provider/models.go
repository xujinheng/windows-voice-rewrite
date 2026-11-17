package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	coreconfig "voicerewrite/internal/core/config"
)

type modelItem struct {
	ID string `json:"id"`
}

// ListModels is the single model-list reader for both native hosts.
// Keep provider response order, deduplicate IDs and bound untrusted results.
func ListModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	if err := coreconfig.ValidateBaseURL(baseURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Endpoint(baseURL, "/models"), nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := RequestClient(10 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接模型 API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, HTTPError("查询模型列表失败", resp.StatusCode)
	}
	var response struct {
		Data []modelItem `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&response); err != nil {
		return nil, fmt.Errorf("无效的模型列表 JSON: %w", err)
	}
	seen := make(map[string]struct{})
	models := make([]string, 0, len(response.Data))
	for _, entry := range response.Data {
		// Reject control characters before trimming: a newline-prefixed ID
		// must not silently turn into a different selectable model.
		if strings.IndexFunc(entry.ID, unicode.IsControl) >= 0 {
			continue
		}
		id := strings.TrimSpace(entry.ID)
		if id == "" || len(id) > coreconfig.MaxModelIDBytes {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	return models, nil
}

func DiscoverModel(ctx context.Context, cfg coreconfig.Config) (string, error) {
	m := strings.TrimSpace(cfg.TextModel)
	if m != "" && !strings.EqualFold(m, "auto") {
		return m, nil
	}
	models, err := ListModels(ctx, cfg.LLMBaseURL, cfg.LLMAPIKey)
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, id := range models {
		x := strings.ToLower(id)
		if strings.Contains(x, "asr") || strings.Contains(x, "whisper") ||
			strings.Contains(x, "embed") || strings.Contains(x, "rerank") ||
			strings.Contains(x, "tts") {
			continue
		}
		candidates = append(candidates, id)
	}
	if len(candidates) == 0 {
		return "", errors.New("没有发现可用的文本模型；请在连接设置中填写明确的文本模型 ID，或确认 API 提供 /v1/models")
	}
	sort.SliceStable(candidates, func(i, j int) bool { return textModelScore(candidates[i]) > textModelScore(candidates[j]) })
	return candidates[0], nil
}

func textModelScore(s string) int {
	l := strings.ToLower(s)
	n := 0
	if strings.Contains(l, "instruct") {
		n += 10
	}
	if strings.Contains(l, "chat") {
		n += 6
	}
	if strings.Contains(l, "vision") || strings.Contains(l, "vl") {
		n -= 3
	}
	return n
}
