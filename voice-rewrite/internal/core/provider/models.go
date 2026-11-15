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

	coreconfig "voicerewrite/internal/core/config"
)

type modelItem struct {
	ID string `json:"id"`
}

func DiscoverModel(ctx context.Context, cfg coreconfig.Config) (string, error) {
	m := strings.TrimSpace(cfg.TextModel)
	if m != "" && !strings.EqualFold(m, "auto") {
		return m, nil
	}
	req, e := http.NewRequestWithContext(ctx, "GET", Endpoint(cfg.LLMBaseURL, "/models"), nil)
	if e != nil {
		return "", e
	}
	if cfg.LLMAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.LLMAPIKey)
	}
	resp, e := RequestClient(10 * time.Second).Do(req)
	if e != nil {
		return "", fmt.Errorf("无法连接文本处理 API: %w", e)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", HTTPError("查询文本模型列表失败", resp.StatusCode)
	}
	var v struct {
		Data []modelItem `json:"data"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); e != nil {
		return "", e
	}
	var candidates []string
	for _, i := range v.Data {
		x := strings.ToLower(i.ID)
		if strings.Contains(x, "asr") || strings.Contains(x, "whisper") || strings.Contains(x, "embed") || strings.Contains(x, "rerank") || strings.Contains(x, "tts") {
			continue
		}
		candidates = append(candidates, i.ID)
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
