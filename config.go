package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// Rule — что вставлять при двойном/тройном нажатии одной клавиши
type Rule struct {
	Disabled      bool   `json:"disabled,omitempty"`
	Layout        uint16 `json:"layout,omitempty"` // 0 — любая, иначе Windows LANGID
	Shift         bool   `json:"shift,omitempty"`
	Key           uint16 `json:"key"`            // virtual-key код клавиши (32 — пробел)
	DoubleText    string `json:"double_text"`    // что вставить вместо двух нажатий ("" — ничего не делать)
	TripleEnabled bool   `json:"triple_enabled"` // реагировать ли на третье нажатие
	TripleText    string `json:"triple_text"`    // что вставить вместо трёх нажатий
}

// Config — пользовательские настройки, хранятся в %APPDATA%\SpaceDot\config.json
type Config struct {
	ExcludedApps []string `json:"excluded_apps,omitempty"`
	Rules        []Rule   `json:"rules"`
	IntervalMs   int      `json:"interval_ms"` // макс. пауза между нажатиями

	AutoUpdate      bool   `json:"auto_update"`       // ставить обновления с GitHub без вопросов
	LastSeenVersion string `json:"last_seen_version"` // версия, для которой уже показали «Что нового»
}

func defaultRule() Rule {
	return Rule{Key: 32, DoubleText: ". ", TripleText: "! "}
}

func defaultConfig() Config {
	return Config{
		Rules:      []Rule{defaultRule()},
		IntervalMs: 500,
	}
}

// ruleFor возвращает правило для клавиши или nil
func (c *Config) ruleFor(vk uint16, shift bool) *Rule {
	return c.ruleForLayout(vk, shift, 0)
}

func (c *Config) ruleForLayout(vk uint16, shift bool, layout uint16) *Rule {
	var fallback *Rule
	for i := range c.Rules {
		r := &c.Rules[i]
		if r.Disabled || r.Key != vk || r.Shift != shift {
			continue
		}
		if r.Layout != 0 && r.Layout == layout {
			return r
		}
		if r.Layout == 0 {
			fallback = r
		}
	}
	return fallback
}

func (c *Config) excludes(app string) bool {
	for _, excluded := range c.ExcludedApps {
		if strings.EqualFold(strings.TrimSpace(excluded), app) {
			return true
		}
	}
	return false
}

// текущие настройки; читаются из горутины хука, пишутся из GUI
var currentConfig atomic.Pointer[Config]
var paused atomic.Bool
var configOverride string

func configPath() (string, error) {
	if configOverride != "" {
		return configOverride, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "SpaceDot", "config.json"), nil
}

func loadConfig() Config {
	cfg := defaultConfig()
	path, err := configPath()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	cfg.Rules = nil
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultConfig()
	}
	if cfg.Rules == nil {
		// старый формат: одна клавиша прямо в корне конфига
		legacy := defaultRule()
		_ = json.Unmarshal(data, &legacy)
		var old struct {
			TriggerKey uint16 `json:"trigger_key"`
		}
		_ = json.Unmarshal(data, &old)
		if old.TriggerKey != 0 {
			legacy.Key = old.TriggerKey
		}
		cfg.Rules = []Rule{legacy}
	}
	if cfg.IntervalMs < 100 || cfg.IntervalMs > 2000 {
		cfg.IntervalMs = defaultConfig().IntervalMs
	}
	return cfg
}

func saveConfig(cfg Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// Сначала полностью записываем новый файл, затем заменяем старый.
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
