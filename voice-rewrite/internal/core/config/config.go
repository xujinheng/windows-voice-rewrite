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
	RecordSecondsMin        = 1
	RecordSecondsMax        = 180
	DefaultRecordSeconds    = 60
	DefaultMaxSelectedRunes = 12000
	MaxSystemPromptBytes = 16384
	MinResponseTokens = 128
	MaxResponseTokens = 4096
	MaxSelectedRunes = 100000
	MinCopyAttempts = 1
	MaxCopyAttempts = 5
	DefaultCopyAttempts = 2
	MinCopyTimeoutMS = 500
	MaxCopyTimeoutMS = 5000
	DefaultCopyTimeoutMS = 750
	// MeetingASRMultipart and MeetingASRLlamaPrefix are the only persisted
	// meeting recognizer names. MeetingASRModes is the settings list order.
	MeetingASRMultipart   = "multipart"
	MeetingASRLlamaPrefix = "llama-prefix"
)

func MeetingASRModes() []string {
	return []string{MeetingASRMultipart, MeetingASRLlamaPrefix}
}

func MeetingASRModeAt(index int) string {
	modes := MeetingASRModes()
	if index < 0 || index >= len(modes) {
		return MeetingASRMultipart
	}
	return modes[index]
}

func MeetingASRModeIndex(mode string) int {
	for i, item := range MeetingASRModes() {
		if item == mode {
			return i
		}
	}
	return 0
}

func MeetingASRModeTitle(mode string) string {
	switch mode {
	case MeetingASRMultipart:
		return "分段音频识别"
	case MeetingASRLlamaPrefix:
		return "累积音频 / 前缀续写"
	default:
		return ""
	}
}

// ChooseAudioDevice keeps the current device unless the combo index is in range.
// The second result is whether the chosen id differs from the current one.
func ChooseAudioDevice(current, index int, selectable bool, ids []int) (int, bool) {
	if !selectable || index < 0 || index >= len(ids) {
		return current, false
	}
	next := ids[index]
	return next, next != current
}

func MeetingASRModeNote() string {
	return MeetingASRLlamaPrefix + " 重复提交累积音频并延续文本前缀；不是原生编码器流式识别。需服务支持该协议。保存会取消当前处理并重新开始。"
}

func Defaults() Config {
	return Config{
		MeetingASRMode:         MeetingASRLlamaPrefix,
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
		MaxSelectedRunes:       DefaultMaxSelectedRunes,
		MaxResponseTokens:      1200,
		SelectionCopyAttempts:  DefaultCopyAttempts,
		SelectionCopyTimeoutMS: DefaultCopyTimeoutMS,
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

// KnownMeetingASRMode is the persisted meeting recognizer choice.
func KnownMeetingASRMode(mode string) bool {
	for _, known := range MeetingASRModes() {
		if mode == known {
			return true
		}
	}
	return false
}

// Normalize applies the persisted configuration's defaults and validation.
// It does not inspect or transform secrets.
func Normalize(cfg Config) (Config, error) {
	defaults := Defaults()
	if !KnownMeetingASRMode(cfg.MeetingASRMode) {
		return cfg, fmt.Errorf("meeting_asr_mode 必须是 %s 或 %s", MeetingASRLlamaPrefix, MeetingASRMultipart)
	}
	if cfg.ASRModel == "" {
		cfg.ASRModel = defaults.ASRModel
	}
	if cfg.HotkeyRecord == "" {
		cfg.HotkeyRecord = defaults.HotkeyRecord
	}
	if cfg.DictationHotkey == "" {
		cfg.DictationHotkey = defaults.DictationHotkey
	}
	if cfg.RecordMode == "" {
		cfg.RecordMode = defaults.RecordMode
	}
	if len(cfg.SystemPrompt) > MaxSystemPromptBytes {
		return cfg, errors.New("system_prompt 内容过长")
	}
	if cfg.RecordMode != "toggle" && cfg.RecordMode != "hold" {
		return cfg, fmt.Errorf("invalid record_mode %q: use toggle or hold", cfg.RecordMode)
	}
	if cfg.HotkeyExit == "" {
		cfg.HotkeyExit = defaults.HotkeyExit
	}
	if cfg.AudioDeviceID < -1 || cfg.AudioDeviceID > 255 {
		return cfg, fmt.Errorf("录音设备编号无效：%d", cfg.AudioDeviceID)
	}
	if cfg.MaxResponseTokens < MinResponseTokens || cfg.MaxResponseTokens > MaxResponseTokens {
		cfg.MaxResponseTokens = defaults.MaxResponseTokens
	}
	if cfg.MaxRecordSeconds < RecordSecondsMin || cfg.MaxRecordSeconds > RecordSecondsMax {
		cfg.MaxRecordSeconds = DefaultRecordSeconds
	}
	if cfg.MaxSelectedRunes <= 0 || cfg.MaxSelectedRunes > MaxSelectedRunes {
		cfg.MaxSelectedRunes = DefaultMaxSelectedRunes
	}
	if cfg.SelectionCopyAttempts < MinCopyAttempts || cfg.SelectionCopyAttempts > MaxCopyAttempts {
		cfg.SelectionCopyAttempts = 3
	}
	if cfg.SelectionCopyTimeoutMS < MinCopyTimeoutMS || cfg.SelectionCopyTimeoutMS > MaxCopyTimeoutMS {
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

// ValidateBaseURL rejects credentials, queries, fragments, non-HTTP schemes,
// and percent escapes that the Go URL parser rejects.
func ValidateBaseURL(base string) error {
	trimmed := strings.TrimSpace(base)
	u, err := url.Parse(trimmed)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || !BaseURLEscapesOK(trimmed) {
		return errors.New("服务地址必须是无用户名、查询参数或锚点的 http(s) API 基础 URL")
	}
	return nil
}

// BaseURLEscapesOK is the percent-escape rule shared with the macOS settings check.
// A % must be followed by two hex digits. In the host, a decoded byte below 0x80
// is rejected, except the encoded percent %25.
func BaseURLEscapesOK(raw string) bool {
	if !percentHexOK(raw, false) {
		return false
	}
	return percentHexOK(baseURLHost(raw), true)
}

func baseURLHost(raw string) string {
	i := strings.Index(raw, "://")
	if i < 0 {
		return ""
	}
	rest := raw[i+3:]
	if cut := strings.IndexAny(rest, "/?#"); cut >= 0 {
		rest = rest[:cut]
	}
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	return rest
}

func percentHexOK(s string, host bool) bool {
	for i := 0; i < len(s); {
		if s[i] != '%' {
			i++
			continue
		}
		if i+2 >= len(s) || hexNibble(s[i+1]) < 0 || hexNibble(s[i+2]) < 0 {
			return false
		}
		if host && s[i:i+3] != "%25" && hexNibble(s[i+1]) < 8 {
			return false
		}
		i += 3
	}
	return true
}

func hexNibble(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}

const (
	MaxEndpointBytes   = 500
	MaxModelIDBytes    = 150
	MaxCredentialBytes = 4000
	CredentialTooLong  = "输入内容过长"
)

// StoredCredentialTooLong reports that the credential bytes about to be stored
// exceed the settings limit. The caller passes the stored form: macOS passes
// the typed field, and Windows passes the trimmed field it writes.
func StoredCredentialTooLong(value string) bool {
	return len(value) > MaxCredentialBytes
}

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
		return Endpoints{}, errors.New(CredentialTooLong)
	}
	if err := ValidateBaseURL(asrURL); err != nil {
		return Endpoints{}, errors.New("请输入有效的 http:// 或 https:// 服务地址")
	}
	if err := ValidateBaseURL(llmURL); err != nil {
		return Endpoints{}, errors.New("请输入有效的 http:// 或 https:// 服务地址")
	}
	defaults := Defaults()
	if asrModel == "" {
		asrModel = defaults.ASRModel
	}
	if textModel == "" {
		textModel = defaults.TextModel
	}
	return Endpoints{ASRBaseURL: asrURL, ASRModel: asrModel, LLMBaseURL: llmURL, TextModel: textModel}, nil
}
