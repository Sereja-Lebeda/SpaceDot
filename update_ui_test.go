package main

import (
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

type updateTestTransport func(*http.Request) (*http.Response, error)

func (f updateTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBackgroundUpdateFailureVisible(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	s := newSettingsWindow(a)
	original := httpClient
	httpClient = &http.Client{Transport: updateTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Status: "503 Service Unavailable", Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	defer func() { httpClient = original }()
	if err := s.checkForUpdates(false); err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(s.updateStatus.Text, "503") || !strings.Contains(s.updateStatus.Text, "1–15") {
		t.Fatal(s.updateStatus.Text)
	}
	if s.updateBusy.Load() || s.updateButton.Disabled() {
		t.Fatal("update lock or button not released")
	}
}

func TestRuleCollapseAndLimitedWidth(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	cfg := Config{IntervalMs: 500, Rules: []Rule{{Key: 190, Shift: true, DoubleText: ",", TripleEnabled: true, TripleText: ";"}, {Key: 32, Disabled: true, DoubleText: ". "}}}
	currentConfig.Store(&cfg)
	s := newSettingsWindow(a)
	s.fill(cfg)
	s.win.Resize(fyne.NewSize(1400, 900))
	s.win.Show()
	s.win.Canvas().Capture()
	if s.rows[0].doubleText.Size().Width > 320 {
		t.Fatal("entry stretched", s.rows[0].doubleText.Size())
	}
	if s.rows[1].details.Visible() {
		t.Fatal("disabled rule is expanded")
	}
	expandedHeight := s.rows[0].box.Size().Height
	before := s.rows[0].rule()
	test.Tap(s.rows[0].enabled)
	s.win.Canvas().Capture()
	if s.rows[0].box.Size().Height >= expandedHeight {
		t.Fatal("disabled card did not shrink")
	}
	if s.rows[0].details.Visible() || strings.Contains(s.rows[0].summary.Text, "→") {
		t.Fatal("rule details remain visible")
	}
	test.Tap(s.rows[0].enabled)
	if !s.rows[0].details.Visible() || s.rows[0].rule() != before {
		t.Fatal("rule did not restore")
	}
	if dir := os.Getenv("SPACEDOT_QA_DIR"); dir != "" {
		saveQAImage(t, filepath.Join(dir, "wide.png"), s)
		test.Tap(s.rows[0].enabled)
		saveQAImage(t, filepath.Join(dir, "collapsed.png"), s)
		s.win.Resize(fyne.NewSize(720, 740))
		oldPath := configOverride
		configOverride = filepath.Join(t.TempDir(), "config.json")
		s.showWhatsNew(&release{Tag: "v1.1.1", Notes: "## Что нового\n\n- Окно с описанием обновления стало больше.\n- Поля настроек больше не растягиваются на всю ширину.\n- Выключенные правила сворачиваются.\n- Автообновление повторяет попытки при ошибках и показывает причину."})
		configOverride = oldPath
		saveQAImage(t, filepath.Join(dir, "whats-new.png"), s)
	}
}

func saveQAImage(t *testing.T, path string, s *settingsWindow) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, s.win.Canvas().Capture()); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentUpdateRejected(t *testing.T) {
	s := &settingsWindow{}
	s.updateBusy.Store(true)
	if err := s.checkForUpdates(false); err != errUpdateBusy {
		t.Fatal(err)
	}
}
