package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestSettingsRoundTrip(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	s := newSettingsWindow(a)
	cfg := Config{IntervalMs: 450, ExcludedApps: []string{"game.exe"}, Rules: []Rule{
		{Key: 190, Shift: true, DoubleText: ",", TripleEnabled: true, TripleText: ";", Layout: 0x419},
		{Key: 32, Disabled: true, DoubleText: ". "},
	}}
	s.fill(cfg)
	if len(s.rows) != 2 {
		t.Fatal("missing rows")
	}
	for i, row := range s.rows {
		if got := row.rule(); got != cfg.Rules[i] {
			t.Fatalf("rule mismatch: %+v", got)
		}
	}
	test.Tap(s.pauseButton)
	if !paused.Load() {
		t.Fatal("pause did not activate")
	}
	test.Tap(s.pauseButton)
	if paused.Load() {
		t.Fatal("pause did not deactivate")
	}
	row := s.addRow(Rule{})
	s.startCapture(row)
	s.cancelCapture()
	if len(s.rows) != 2 || captureKey.Load() != nil {
		t.Fatal("capture cancellation failed")
	}
}

func TestSaveSettings(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	oldPath := configOverride
	configOverride = filepath.Join(t.TempDir(), "config.json")
	defer func() { configOverride = oldPath }()
	cfg := Config{IntervalMs: 500, LastSeenVersion: version, Rules: []Rule{{Key: 190, Shift: true, DoubleText: ","}}}
	currentConfig.Store(&cfg)
	s := newSettingsWindow(a)
	s.fill(cfg)
	s.autostart.SetChecked(autostartEnabled())
	s.excludedApps.SetText(" game.exe , editor.exe ")
	s.save()
	got := loadConfig()
	if !reflect.DeepEqual(got.ExcludedApps, []string{"game.exe", "editor.exe"}) || got.Rules[0] != cfg.Rules[0] {
		t.Fatal(got)
	}
	// Повторное сохранение должно атомарно заменить уже существующий файл.
	s.rows[0].doubleText.SetText(";")
	s.save()
	if loadConfig().Rules[0].DoubleText != ";" {
		t.Fatal("overwrite failed")
	}
	if s.status.Text != "✓ Настройки сохранены и применены" {
		t.Fatal(s.status.Text)
	}
}
