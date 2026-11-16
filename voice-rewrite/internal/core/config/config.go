package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// VoiceRewrite never sends audio or selected text to a provider other than the
// two HTTP endpoints explicitly configured by the user.
type Config struct {
	MeetingASRMode         string `json:"meeting_asr_mode"`
	MeetingBackground      string `json:"meeting_background"`
	MeetingTerms           string `json:"meeting_terms"`
	ASRBaseURL             string `json:"asr_base_url"`
	LLMBaseURL             string `json:"llm_base_url"`
	TextModel              string `json:"text_model"`
	ASRAPIKey              string `json:"asr_api_key"`
	LLMAPIKey              string `json:"llm_api_key"`
	ASRModel               string `json:"asr_model"`
	AudioDeviceID          int    `json:"audio_device_id"`
	HotkeyRecord           string `json:"hotkey_record"`
	DictationHotkey        string `json:"dictation_hotkey"`
	RecordMode             string `json:"record_mode"`
	HotkeyExit             string `json:"hotkey_exit"`
	SystemPrompt           string `json:"system_prompt"`
	MaxResponseTokens      int    `json:"max_response_tokens"`
	MaxRecordSeconds       int    `json:"max_record_seconds"`
	MaxSelectedRunes       int    `json:"max_selected_characters"`
	SelectionCopyAttempts  int    `json:"selection_copy_attempts"`
	SelectionCopyTimeoutMS int    `json:"selection_copy_timeout_ms"`
}

const (
	RecordSecondsMin     = 1
	RecordSecondsMax     = 180
	DefaultRecordSeconds = 60
)

func Defaults() Config {
	return Config{
		MeetingASRMode:         "llama-prefix",
		ASRBaseURL:             "http://127.0.0.1:8080/v1",
		LLMBaseURL:             "http://127.0.0.1:1234/v1",
		TextModel:              "auto",
		ASRModel:               "whisper-1",
		AudioDeviceID:          -1,
		HotkeyRecord:           "Mouse5",
		DictationHotkey:        "Mouse4",
		RecordMode:             "hold",
		HotkeyExit:             "Ctrl+Alt+F10",
		MaxRecordSeconds:       DefaultRecordSeconds,
		MaxSelectedRunes:       12000,
		MaxResponseTokens:      1200,
		SelectionCopyAttempts:  2,
		SelectionCopyTimeoutMS: 750,
	}
}

// Parse overlays JSON onto defaults. Credentials remain opaque until a native
// adapter decrypts them; callers must Normalize the decrypted configuration.
func Parse(data []byte) (Config, error) {
	cfg := Defaults()
	err := json.Unmarshal(data, &cfg)
	return cfg, err
}

// Marshal serializes a configuration without touching the filesystem or keys.
func Marshal(cfg Config) ([]byte, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Normalize applies the persisted configuration's defaults and validation.
// It does not inspect or transform secrets.
func Normalize(cfg Config) (Config, error) {
	if cfg.MeetingASRMode != "llama-prefix" && cfg.MeetingASRMode != "multipart" {
		return cfg, errors.New("meeting_asr_mode 必须是 llama-prefix 或 multipart")
	}
	if cfg.ASRModel == "" {
		cfg.ASRModel = "whisper-1"
	}
	if cfg.HotkeyRecord == "" {
		cfg.HotkeyRecord = "Mouse5"
	}
	if cfg.DictationHotkey == "" {
		cfg.DictationHotkey = "Mouse4"
	}
	if cfg.RecordMode == "" {
		cfg.RecordMode = "hold"
	}
	if cfg.RecordMode != "toggle" && cfg.RecordMode != "hold" {
		return cfg, fmt.Errorf("invalid record_mode %q: use toggle or hold", cfg.RecordMode)
	}
	if cfg.HotkeyExit == "" {
		cfg.HotkeyExit = "Ctrl+Alt+F10"
	}
	if cfg.AudioDeviceID < -1 || cfg.AudioDeviceID > 255 {
		return cfg, fmt.Errorf("录音设备编号无效：%d", cfg.AudioDeviceID)
	}
	if cfg.MaxResponseTokens < 128 || cfg.MaxResponseTokens > 4096 {
		cfg.MaxResponseTokens = 1200
	}
	if cfg.MaxRecordSeconds < RecordSecondsMin || cfg.MaxRecordSeconds > RecordSecondsMax {
		cfg.MaxRecordSeconds = DefaultRecordSeconds
	}
	if cfg.MaxSelectedRunes <= 0 || cfg.MaxSelectedRunes > 100000 {
		cfg.MaxSelectedRunes = 12000
	}
	if cfg.SelectionCopyAttempts < 1 || cfg.SelectionCopyAttempts > 5 {
		cfg.SelectionCopyAttempts = 3
	}
	if cfg.SelectionCopyTimeoutMS < 500 || cfg.SelectionCopyTimeoutMS > 5000 {
		cfg.SelectionCopyTimeoutMS = 1800
	}
	cfg.ASRBaseURL = strings.TrimSpace(cfg.ASRBaseURL)
	cfg.LLMBaseURL = strings.TrimSpace(cfg.LLMBaseURL)
	for _, base := range []string{cfg.ASRBaseURL, cfg.LLMBaseURL} {
		if err := ValidateBaseURL(base); err != nil {
			return cfg, fmt.Errorf("无效的 API 地址: %w", err)
		}
	}
	return cfg, nil
}

// TransformSecrets applies an OS-supplied credential operation to both keys.
// The core never selects an encryption provider or stores credentials.
func TransformSecrets(cfg Config, transform func(string) (string, error)) (Config, error) {
	var err error
	if cfg.ASRAPIKey, err = transform(cfg.ASRAPIKey); err != nil {
		return cfg, err
	}
	if cfg.LLMAPIKey, err = transform(cfg.LLMAPIKey); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// ValidateBaseURL rejects credentials, queries, fragments and non-HTTP schemes
// before a configured endpoint can be used to carry private data.
func ValidateBaseURL(base string) error {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("服务地址必须是无用户名、查询参数或锚点的 http(s) API 基础 URL")
	}
	return nil
}

const (
	MaxEndpointBytes   = 500
	MaxModelIDBytes    = 150
	MaxCredentialBytes = 4000
)

// Endpoints is the non-secret ASR and LLM identity stored by both desktop hosts.
type Endpoints struct {
	ASRBaseURL string
	ASRModel   string
	LLMBaseURL string
	TextModel  string
}

// CanonicalEndpoints trims addresses, drops a trailing slash, and fills empty
// model names with the product defaults. It rejects addresses ValidateBaseURL
// rejects and fields that exceed the settings form limits.
func CanonicalEndpoints(asrURL, asrModel, llmURL, textModel string) (Endpoints, error) {
	asrURL = strings.TrimRight(strings.TrimSpace(asrURL), "/")
	llmURL = strings.TrimRight(strings.TrimSpace(llmURL), "/")
	asrModel = strings.TrimSpace(asrModel)
	textModel = strings.TrimSpace(textModel)
	if len(asrURL) > MaxEndpointBytes || len(llmURL) > MaxEndpointBytes || len(asrModel) > MaxModelIDBytes || len(textModel) > MaxModelIDBytes {
		return Endpoints{}, errors.New("输入内容过长")
	}
	if err := ValidateBaseURL(asrURL); err != nil {
		return Endpoints{}, errors.New("请输入有效的 http:// 或 https:// 服务地址")
	}
	if err := ValidateBaseURL(llmURL); err != nil {
		return Endpoints{}, errors.New("请输入有效的 http:// 或 https:// 服务地址")
	}
	if asrModel == "" {
		asrModel = "whisper-1"
	}
	if textModel == "" {
		textModel = "auto"
	}
	return Endpoints{ASRBaseURL: asrURL, ASRModel: asrModel, LLMBaseURL: llmURL, TextModel: textModel}, nil
}
