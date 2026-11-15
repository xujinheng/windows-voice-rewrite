package config

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
		MaxRecordSeconds:       60,
		MaxSelectedRunes:       12000,
		MaxResponseTokens:      1200,
		SelectionCopyAttempts:  2,
		SelectionCopyTimeoutMS: 750,
	}
}
