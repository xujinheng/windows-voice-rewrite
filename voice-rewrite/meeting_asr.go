package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type meetingASRState struct{ raw, fixed string }

func meetingJSON(ctx context.Context, cfg Config, url string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.ASRAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.ASRAPIKey)
	}
	resp, err := requestClient(16 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return meetingASRHTTPError(resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
func meetingASRHTTPError(status int) error {
	if status == 404 || status == 405 || status == 400 || status == 501 {
		return fmt.Errorf("ASR 续写接口不可用（HTTP %d），请更新 oneclick/llama-server，或在会议设置选择分段识别", status)
	}
	return providerHTTPError("ASR", status)
}
func (s *meetingASRState) prefix(ctx context.Context, cfg Config) (string, error) {
	marker := strings.Index(s.raw, "<asr_text>")
	if marker < 0 {
		return "", nil
	}
	header := s.raw[:marker+len("<asr_text>")]
	text := s.raw[marker+len("<asr_text>"):]
	base := strings.TrimSuffix(strings.TrimRight(cfg.ASRBaseURL, "/"), "/v1")
	var tokens struct {
		Tokens []int `json:"tokens"`
	}
	if err := meetingJSON(ctx, cfg, base+"/tokenize", map[string]any{"content": text, "add_special": false}, &tokens); err != nil {
		return "", err
	}
	if len(tokens.Tokens) <= 5 {
		return "", nil
	}
	for n := len(tokens.Tokens) - 5; n > 0; n-- {
		var decoded struct {
			Content string `json:"content"`
		}
		if err := meetingJSON(ctx, cfg, base+"/detokenize", map[string]any{"tokens": tokens.Tokens[:n]}, &decoded); err != nil {
			return "", err
		}
		if !strings.ContainsRune(decoded.Content, '\ufffd') {
			return header + decoded.Content, nil
		}
	}
	return "", nil
}

// llama.cpp consumes a bounded cumulative audio window. Text prefix rollback
// reduces decoder work; it does not provide incremental audio encoder caching.
func (s *meetingASRState) transcribe(ctx context.Context, cfg Config, pcm []byte, emit func(string)) (string, error) {
	if cfg.MeetingASRMode == "multipart" {
		result, err := transcribe(ctx, cfg, pcm)
		if err == nil {
			emit(result)
		}
		return result, err
	}
	prefix, err := s.prefix(ctx, cfg)
	if err != nil {
		return "", err
	}
	s.fixed = cleanASR(prefix)
	wav, err := encodeWAV(pcm)
	if err != nil {
		return "", err
	}
	messages := []map[string]any{}
	if cfg.MeetingTerms != "" {
		messages = append(messages, map[string]any{"role": "system", "content": "Vocabulary for spelling only: " + cfg.MeetingTerms})
	}
	messages = append(messages, map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_audio", "input_audio": map[string]string{"data": base64.StdEncoding.EncodeToString(wav), "format": "wav"}}}})
	if prefix != "" {
		messages = append(messages, map[string]any{"role": "assistant", "content": prefix})
	}
	data, err := json.Marshal(map[string]any{"model": cfg.ASRModel, "messages": messages, "stream": true, "temperature": 0, "max_tokens": 256, "cache_prompt": true})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(cfg.ASRBaseURL, "/chat/completions"), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.ASRAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.ASRAPIKey)
	}
	resp, err := requestClient(16 * time.Second).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", meetingASRHTTPError(resp.StatusCode)
	}
	observed := false
	var raw strings.Builder
	raw.WriteString(prefix)
	publish := func() {
		text := raw.String()
		// Until the metadata delimiter arrives, language detection is not transcript.
		if (strings.HasPrefix(strings.TrimSpace(text), "language") || strings.HasPrefix("language ", strings.TrimSpace(text))) && !strings.Contains(text, "<asr_text>") {
			return
		}
		if clean := cleanASR(text); clean != "" && ctx.Err() == nil {
			emit(clean)
		}
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		var result meetingCompletion
		if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
			return "", err
		}
		if result.Error != nil {
			return "", errors.New("ASR 服务返回错误")
		}
		if len(result.Choices) > 0 {
			observed = result.Choices[0].Message.Content != "" || result.Choices[0].FinishReason == "stop"
			if reason := result.Choices[0].FinishReason; reason != "" && reason != "stop" {
				return "", errors.New("ASR 输出未完整结束")
			}
			raw.WriteString(result.Choices[0].Message.Content)
			publish()
		}
	} else {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
		scanner.Buffer(make([]byte, 8192), 128<<10)
		done := false
		for scanner.Scan() {
			data, ok := strings.CutPrefix(scanner.Text(), "data:")
			if !ok {
				continue
			}
			data = strings.TrimSpace(data)
			if data == "[DONE]" {
				done = true
				break
			}
			var chunk meetingCompletion
			if err = json.Unmarshal([]byte(data), &chunk); err != nil {
				return "", errors.New("ASR 流式响应格式错误")
			}
			if chunk.Error != nil {
				return "", errors.New("ASR 服务返回流式错误")
			}
			if len(chunk.Choices) > 0 {
				observed = observed || chunk.Choices[0].Delta.Content != "" || chunk.Choices[0].FinishReason == "stop"
				if chunk.Choices[0].FinishReason == "length" {
					return "", errors.New("ASR 输出达到长度限制，请缩短音频段")
				}
				raw.WriteString(chunk.Choices[0].Delta.Content)
				publish()
			}
		}
		if err = scanner.Err(); err != nil {
			return "", err
		}
		if !done {
			return "", errors.New("ASR 响应提前结束")
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if !observed {
		return "", errors.New("ASR 返回空响应，未提交转录")
	}
	text := cleanASR(raw.String())
	if text == "" || strings.HasPrefix(text, "language") {
		return "", errors.New("ASR 未返回有效转录")
	}
	s.raw = raw.String()
	return text, nil
}
