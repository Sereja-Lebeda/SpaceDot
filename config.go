package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
)

// Rule — что вставлять при двойном/тройном нажатии одной клавиши
type Rule struct {
	Key           uint16 `json:"key"`            // virtual-key код клавиши (32 — пробел)
	DoubleText    string `json:"double_text"`    // что вставить вместо двух нажатий ("" — ничего не делать)
	TripleEnabled bool   `json:"triple_enabled"` // реагировать ли на третье нажатие
	TripleText    string `json:"triple_text"`    // что вставить вместо трёх нажатий
}

// Config — пользовательские настройки, хранятся в %APPDATA%\SpaceDot\config.json
type Config struct {
	Rules      []Rule `json:"rules"`
	IntervalMs int    `json:"interval_ms"` // макс. пауза между нажатиями
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
func (c *Config) ruleFor(vk uint16) *Rule {
	for i := range c.Rules {
		if c.Rules[i].Key == vk {
			return &c.Rules[i]
		}
	}
	return nil
}

// текущие настройки; читаются из горутины хука, пишутся из GUI
var currentConfig atomic.Pointer[Config]

func configPath() (string, error) {
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
	_ = json.Unmarshal(data, &cfg)
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
	return os.WriteFile(path, data, 0o644)
}
