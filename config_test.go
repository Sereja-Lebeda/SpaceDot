package main

import (
	"encoding/json"
	"testing"
)

func TestShiftRules(t *testing.T) {
	cfg := Config{Rules: []Rule{{Key: 190, DoubleText: "."}, {Key: 190, Shift: true, DoubleText: ","}}}
	for _, shift := range []bool{false, true} {
		rule := cfg.ruleFor(190, shift)
		if rule == nil || rule.Shift != shift {
			t.Fatalf("incorrect match for shift=%v: %+v", shift, rule)
		}
	}
	if cfg.ruleFor(32, true) != nil {
		t.Fatal("unexpected match")
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if rule := restored.ruleFor(190, true); rule == nil || rule.DoubleText != "," {
		t.Fatal("lost Shift rule after serialization")
	}
}

func TestLegacyRuleWithoutShift(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"rules":[{"key":32,"double_text":". "}]}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ruleFor(32, false) == nil || cfg.ruleFor(32, true) != nil {
		t.Fatal("legacy rule must only match unmodified key")
	}
}
