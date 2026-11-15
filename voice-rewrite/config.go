package main

import (
	coreconfig "voicerewrite/internal/core/config"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"voicerewrite/internal/core/provider"
)

// Config is shared with core services. This package only owns OS-specific
// configuration paths, credential encryption and persistence.
type Config = coreconfig.Config

func defaults() Config { return coreconfig.Defaults() }

// mouseSideButton reports the Windows XBUTTON identifier. Mouse4 is XBUTTON1
// and Mouse5 is XBUTTON2, independent of how the mouse is physically arranged.
func mouseSideButton(s string) (uint16, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mouse4", "xbutton1":
		return 1, true
	case "mouse5", "xbutton2":
		return 2, true
	default:
		return 0, false
	}
}

func configPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return filepath.Join(os.TempDir(), "VoiceRewrite", "config.json")
	}
	return filepath.Join(base, "VoiceRewrite", "config.json")
}

func loadConfig() (Config, error) {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return defaults(), fmt.Errorf("无法创建用户配置目录：%w", err)
	}
	cfg := defaults()
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		blob, _ := json.MarshalIndent(cfg, "", "  ")
		if e := os.WriteFile(p, append(blob, '\n'), 0600); e != nil {
			return cfg, fmt.Errorf("无法创建配置文件：%w", e)
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("配置文件 JSON 错误: %w", err)
	}
	if cfg, err = decryptConfigSecrets(cfg); err != nil {
		return cfg, fmt.Errorf("无法解锁 API 凭据（当前 Windows 用户）: %w", err)
	}
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
	if cfg.MaxRecordSeconds <= 0 || cfg.MaxRecordSeconds > 180 {
		cfg.MaxRecordSeconds = 60
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
	for _, x := range []string{cfg.ASRBaseURL, cfg.LLMBaseURL} {
		if err := provider.ValidateBaseURL(x); err != nil {
			return cfg, fmt.Errorf("无效的 API 地址: %w", err)
		}
	}
	return cfg, nil
}
