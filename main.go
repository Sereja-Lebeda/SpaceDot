package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"fyne.io/systray"
)

//go:embed icon.ico
var iconData []byte

func main() {
	showSettings := flag.Bool("settings", false, "Открыть настройки при запуске")
	flag.StringVar(&configOverride, "config", "", "Путь к config.json (по умолчанию AppData)")
	flag.Parse()

	cfg := loadConfig()
	currentConfig.Store(&cfg)

	a := app.NewWithID("SpaceDot")
	icon := fyne.NewStaticResource("icon.ico", iconData)
	a.SetIcon(icon)

	settings := newSettingsWindow(a)
	events, stopHook, hookErr := startInputHook()
	if hookErr == nil {
		go listenKeys(events)
		defer stopHook()
	}

	if desk, ok := a.(desktop.App); ok {
		quit := fyne.NewMenuItem("Выход", nil)
		// Иначе Fyne добавит в меню свой пункт «Выйти»
		quit.IsQuit = true

		desk.SetSystemTrayIcon(icon)
		settings.trayMenu = fyne.NewMenu("SpaceDot",
			fyne.NewMenuItem("Настройки", settings.show),
			settings.pauseMenu,
			fyne.NewMenuItemSeparator(),
			quit,
		)
		desk.SetSystemTrayMenu(settings.trayMenu)
	}

	a.Lifecycle().SetOnStarted(func() {
		// Fyne на Windows не задаёт подсказку трея — ставим только название
		settings.started = true
		settings.refreshPause()

		// ЛКМ по иконке → настройки; ПКМ по-прежнему открывает меню
		systray.SetOnTapped(func() { fyne.Do(settings.show) })

		settings.whatsNewOnStart()
		if *showSettings || hookErr != nil {
			settings.show()
		}
		if hookErr != nil {
			dialog.ShowError(fmt.Errorf("Не удалось включить обработку клавиш: %w", hookErr), settings.win)
		}
		go settings.updateLoop()
	})
	go cleanupOldExe()

	// a.Run блокирует главный поток до a.Quit()
	a.Run()
}

// ruleRow — блок настроек одной клавиши в окне
type ruleRow struct {
	enabled       *widget.Check
	layout        *widget.Select
	layoutCodes   []uint16
	summary       *widget.Label
	key           uint16
	shift         bool
	keyButton     *widget.Button
	doubleText    *widget.Entry
	tripleEnabled *widget.Check
	tripleText    *widget.Entry
	box           fyne.CanvasObject
}

func (r *ruleRow) setKey(vk uint16, shift bool) {
	r.key = vk
	r.shift = shift
	r.keyButton.SetText(triggerName(vk, shift))
	r.keyButton.Enable()
	r.refreshSummary()
}

func (r *ruleRow) rule() Rule {
	return Rule{
		Disabled:      !r.enabled.Checked,
		Layout:        r.layoutCodes[r.layout.SelectedIndex()],
		Key:           r.key,
		Shift:         r.shift,
		DoubleText:    r.doubleText.Text,
		TripleEnabled: r.tripleEnabled.Checked,
		TripleText:    r.tripleText.Text,
	}
}

type settingsWindow struct {
	app fyne.App
	win fyne.Window

	rows         []*ruleRow
	rowsBox      *fyne.Container
	capturing    *ruleRow // строка, ждущая нажатия клавиши
	interval     *widget.Entry
	autostart    *widget.Check
	autoUpdate   *widget.Check
	updateButton *widget.Button
	update       *release // найденное, но ещё не установленное обновление
	excludedApps *widget.Entry
	status       *widget.Label
	pauseButton  *widget.Button
	pauseMenu    *fyne.MenuItem
	trayMenu     *fyne.Menu
	started      bool
}

func newSettingsWindow(a fyne.App) *settingsWindow {
	s := &settingsWindow{app: a, win: a.NewWindow("SpaceDot — настройки")}

	s.rowsBox = container.NewVBox()
	s.status = widget.NewLabel("Изменения применяются после сохранения")
	s.pauseButton = widget.NewButton("", s.togglePause)
	s.pauseMenu = fyne.NewMenuItem("", s.togglePause)
	s.refreshPause()
	s.interval = newEntry()
	s.excludedApps = newEntry()
	s.excludedApps.SetPlaceHolder("Например: game.exe, WindowsTerminal.exe")
	s.autostart = widget.NewCheck("Запускать вместе с Windows", nil)
	s.autoUpdate = widget.NewCheck("Обновлять автоматически", nil)
	s.updateButton = widget.NewButton("", s.onUpdateButton)
	s.resetUpdateButton()

	addRule := widget.NewButtonWithIcon("Добавить сочетание", theme.ContentAddIcon(), func() {
		r := defaultRule()
		r.Key = 0
		r.DoubleText, r.TripleText = "", ""
		row := s.addRow(r)
		s.startCapture(row)
	})

	hint := widget.NewLabel("Пустое поле — оставить обычный ввод. Можно записать Shift + клавишу. Ctrl / Alt / Win не поддерживаются.")
	hint.Wrapping = fyne.TextWrapWord

	common := widget.NewForm(
		widget.NewFormItem("Интервал, мс", s.interval),
		widget.NewFormItem("Исключения", s.excludedApps),
		widget.NewFormItem("", s.autostart),
		widget.NewFormItem("Версия "+version, s.autoUpdate),
		widget.NewFormItem("", s.updateButton),
	)

	save := widget.NewButton("Сохранить", s.save)
	save.Importance = widget.HighImportance

	intro := widget.NewLabel("Два нажатия — ваш текст. Работает во всех приложениях.")
	intro.Wrapping = fyne.TextWrapWord
	top := container.NewVBox(container.NewBorder(nil, nil, nil, s.pauseButton,
		widget.NewLabelWithStyle("SpaceDot", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})), intro)
	preview := widget.NewMultiLineEntry()
	preview.SetPlaceHolder("Сохраните правила и попробуйте сочетание здесь…")
	preview.SetMinRowsVisible(5)
	previewHint := widget.NewLabel("Проверка использует сохранённые правила, выбранную раскладку и состояние паузы. Исключение для SpaceDot.exe действует и здесь.")
	previewHint.Wrapping = fyne.TextWrapWord
	rules := container.NewBorder(nil, container.NewVBox(hint, addRule), nil, nil, container.NewVScroll(s.rowsBox))
	preferences := container.NewVBox(common, widget.NewLabel("Имена exe через запятую. В этих приложениях замены отключены."))
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Правила", theme.ListIcon(), rules),
		container.NewTabItemWithIcon("Проверка", theme.SearchIcon(), container.NewVBox(previewHint, preview,
			widget.NewButton("Очистить", func() { preview.SetText("") }))),
		container.NewTabItemWithIcon("Настройки", theme.SettingsIcon(), container.NewVScroll(preferences)),
	)
	tabs.OnSelected = func(*container.TabItem) { s.cancelCapture() }
	bottom := container.NewVBox(widget.NewSeparator(), s.status,
		container.NewBorder(nil, nil, widget.NewButton("По умолчанию", s.resetDefaults), save))
	s.win.SetContent(container.NewPadded(container.NewBorder(top, bottom, nil, nil, tabs)))
	s.win.Resize(fyne.NewSize(720, 740))
	// Закрытие окна только прячет его — программа продолжает работать в трее
	s.win.SetCloseIntercept(func() {
		s.cancelCapture()
		s.win.Hide()
	})

	return s
}

func (s *settingsWindow) addRow(rule Rule) *ruleRow {
	row := &ruleRow{
		doubleText: newEntry(),
		tripleText: newEntry(),
	}
	row.summary = widget.NewLabel("")
	row.summary.Wrapping = fyne.TextWrapWord
	row.enabled = widget.NewCheck("Включено", func(bool) { row.refreshSummary() })
	row.enabled.SetChecked(!rule.Disabled)
	options, codes := layoutChoices()
	row.layoutCodes = codes
	row.layout = widget.NewSelect(options, func(string) { row.refreshSummary() })
	selected := 0
	for i, code := range codes {
		if code == rule.Layout {
			selected = i
		}
	}
	if rule.Layout != 0 && selected == 0 {
		row.layoutCodes = append(row.layoutCodes, rule.Layout)
		row.layout.Options = append(row.layout.Options, layoutName(rule.Layout))
		selected = len(row.layoutCodes) - 1
	}
	row.layout.SetSelectedIndex(selected)
	row.doubleText.SetPlaceHolder("Например: ,")
	row.tripleText.SetPlaceHolder("Например: ! ")
	row.keyButton = widget.NewButton("", func() { s.startCapture(row) })
	// Поле для тройного нажатия показываем только при включённой галочке
	row.tripleEnabled = widget.NewCheck("", func(on bool) {
		if on {
			row.tripleText.Show()
		} else {
			row.tripleText.Hide()
		}
		row.refreshSummary()
	})
	remove := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() { s.removeRow(row) })

	if rule.Key != 0 {
		row.setKey(rule.Key, rule.Shift)
	}
	row.doubleText.SetText(rule.DoubleText)
	row.tripleText.SetText(rule.TripleText)
	row.tripleEnabled.SetChecked(rule.TripleEnabled)
	row.tripleEnabled.OnChanged(rule.TripleEnabled)
	row.doubleText.OnChanged = func(string) { row.refreshSummary() }
	row.tripleText.OnChanged = func(string) { row.refreshSummary() }

	form := widget.NewForm(
		widget.NewFormItem("Сочетание", row.keyButton),
		widget.NewFormItem("Двойное нажатие →", row.doubleText),
		widget.NewFormItem("Тройное нажатие →", container.NewBorder(nil, nil, row.tripleEnabled, nil, row.tripleText)),
		widget.NewFormItem("Раскладка", row.layout),
	)
	row.box = widget.NewCard("", "", container.NewVBox(container.NewBorder(nil, nil, row.enabled, remove, row.summary), widget.NewSeparator(), form))
	row.refreshSummary()

	s.rows = append(s.rows, row)
	s.rowsBox.Add(row.box)
	return row
}

func (s *settingsWindow) removeRow(row *ruleRow) {
	if s.capturing == row {
		s.cancelCapture()
	}
	for i, r := range s.rows {
		if r == row {
			s.rows = append(s.rows[:i], s.rows[i+1:]...)
			break
		}
	}
	s.rowsBox.Remove(row.box)
}

// startCapture ждёт следующее нажатие через глобальный хук и назначает его клавишей строки
func (s *settingsWindow) startCapture(row *ruleRow) {
	s.cancelCapture()
	s.capturing = row
	row.keyButton.SetText("Буква, цифра, знак или пробел… (Esc — отмена)")
	row.keyButton.Disable()

	cb := func(vk uint16, shift bool) {
		fyne.Do(func() {
			if s.capturing != row {
				return
			}
			s.capturing = nil
			if vk == 0 {
				vk = row.key
				shift = row.shift
			}
			if vk == 0 {
				// новая строка, назначение отменено — убираем её
				s.removeRow(row)
				return
			}
			row.setKey(vk, shift)
		})
	}
	captureKey.Store(&cb)
}

func (s *settingsWindow) cancelCapture() {
	captureKey.Store(nil)
	row := s.capturing
	if row == nil {
		return
	}
	s.capturing = nil
	if row.key == 0 {
		s.removeRow(row)
	} else {
		row.setKey(row.key, row.shift)
	}
}

func (s *settingsWindow) fill(cfg Config) {
	s.cancelCapture()
	s.rows = nil
	s.rowsBox.RemoveAll()
	for _, r := range cfg.Rules {
		s.addRow(r)
	}
	s.interval.SetText(strconv.Itoa(cfg.IntervalMs))
	s.autoUpdate.SetChecked(cfg.AutoUpdate)
	s.excludedApps.SetText(strings.Join(cfg.ExcludedApps, ", "))
	s.status.SetText("Изменения применяются после сохранения")
}

func (s *settingsWindow) show() {
	s.fill(*currentConfig.Load())
	s.autostart.SetChecked(autostartEnabled())
	s.win.Show()
	s.win.RequestFocus()
}

func (s *settingsWindow) resetDefaults() {
	s.fill(defaultConfig())
}

func (s *settingsWindow) save() {
	ms, err := strconv.Atoi(s.interval.Text)
	if err != nil || ms < 100 || ms > 2000 {
		dialog.ShowInformation("Ошибка", "Интервал должен быть числом от 100 до 2000 мс", s.win)
		return
	}
	s.cancelCapture()

	cfg := Config{
		Rules:           []Rule{},
		IntervalMs:      ms,
		AutoUpdate:      s.autoUpdate.Checked,
		LastSeenVersion: currentConfig.Load().LastSeenVersion,
	}
	seen := map[struct {
		key    uint16
		shift  bool
		layout uint16
	}]bool{}
	for _, row := range s.rows {
		name := triggerName(row.key, row.shift)
		id := struct {
			key    uint16
			shift  bool
			layout uint16
		}{row.key, row.shift, row.rule().Layout}
		if seen[id] {
			dialog.ShowError(errors.New("Клавиша «"+name+"» назначена дважды"), s.win)
			return
		}
		seen[id] = true
		cfg.Rules = append(cfg.Rules, row.rule())
	}
	for _, value := range strings.Split(s.excludedApps.Text, ",") {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".exe") || strings.ContainsAny(name, `/\:*?"<>|`) {
			dialog.ShowError(fmt.Errorf("Укажите имя exe без пути: %s", name), s.win)
			return
		}
		cfg.ExcludedApps = append(cfg.ExcludedApps, name)
	}
	if err := saveConfig(cfg); err != nil {
		dialog.ShowError(err, s.win)
		return
	}
	currentConfig.Store(&cfg)
	if s.autostart.Checked != autostartEnabled() {
		if err := setAutostart(s.autostart.Checked); err != nil {
			dialog.ShowError(err, s.win)
			return
		}
	}
	s.status.SetText("✓ Настройки сохранены и применены")
}

func (r *ruleRow) refreshSummary() {
	if r.summary == nil || r.enabled == nil || r.doubleText == nil {
		return
	}
	name := "Новое сочетание"
	if r.key != 0 {
		name = triggerName(r.key, r.shift)
	}
	result := "обычный ввод"
	if r.doubleText.Text != "" {
		result = strconv.Quote(r.doubleText.Text)
	}
	prefix := ""
	if !r.enabled.Checked {
		prefix = "Выключено · "
	}
	if r.layout != nil && r.layout.SelectedIndex() > 0 {
		result += " · " + r.layout.Selected
	}
	if r.tripleEnabled != nil && r.tripleEnabled.Checked {
		result += " · ×3 → " + strconv.Quote(r.tripleText.Text)
	}
	r.summary.SetText(prefix + name + " ×2 → " + result)
}

func (s *settingsWindow) togglePause() {
	paused.Store(!paused.Load())
	s.refreshPause()
}

func (s *settingsWindow) refreshPause() {
	text, tooltip := "Приостановить", "SpaceDot — работает"
	if paused.Load() {
		text, tooltip = "Продолжить", "SpaceDot — пауза"
	}
	s.pauseButton.SetText(text)
	s.pauseMenu.Label = text
	if s.started {
		systray.SetTooltip(tooltip)
	}
	if s.trayMenu != nil {
		s.trayMenu.Refresh()
	}
}

// newEntry — однострочное поле без собственной прокрутки: иначе колесо мыши
// над полем «застревает» в нём, а не прокручивает окно настроек
func newEntry() *widget.Entry {
	e := widget.NewEntry()
	e.Scroll = fyne.ScrollNone
	return e
}
