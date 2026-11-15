package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	coreconfig "voicerewrite/internal/core/config"
)

// The native host owns the configuration location, disk writes, and credential
// protection; the portable core owns JSON representation and validation.
func configPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return filepath.Join(os.TempDir(), "VoiceRewrite", "config.json")
	}
	return filepath.Join(base, "VoiceRewrite", "config.json")
}

func loadConfig() (Config, error) { return loadConfigFrom(configPath()) }

func loadConfigFrom(path string) (Config, error) {
	cfg := defaults()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return cfg, fmt.Errorf("无法创建用户配置目录：%w", err)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data, _ = coreconfig.Marshal(cfg)
		if e := os.WriteFile(path, data, 0600); e != nil {
			return cfg, fmt.Errorf("无法创建配置文件：%w", e)
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	cfg, err = coreconfig.Parse(data)
	if err != nil {
		return cfg, fmt.Errorf("配置文件 JSON 错误: %w", err)
	}
	cfg, err = decryptConfigSecrets(cfg)
	if err != nil {
		return cfg, fmt.Errorf("无法解锁 API 凭据（当前 Windows 用户）: %w", err)
	}
	return coreconfig.Normalize(cfg)
}

// saveConfig writes to a private temporary file then atomically replaces the
// configuration. A failed save never overwrites a working configuration.
func saveConfig(cfg Config) error { return saveConfigTo(configPath(), cfg) }

func saveConfigTo(path string, cfg Config) error {
	// Reject invalid settings before encrypting credentials or touching disk.
	var err error
	cfg, err = coreconfig.Normalize(cfg)
	if err != nil {
		return err
	}
	stored, err := encryptConfigSecrets(cfg)
	if err != nil {
		return fmt.Errorf("加密 API 凭据: %w", err)
	}
	data, err := coreconfig.Marshal(stored)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
