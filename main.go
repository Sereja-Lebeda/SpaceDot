package main

import (
	_ "embed"
	"errors"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"fyne.io/systray"
	"github.com/go-vgo/robotgo"
	hook "github.com/robotn/gohook"
)

//go:embed icon.ico
var iconData []byte

func main() {
	robotgo.KeySleep = 10

	cfg := loadConfig()
	currentConfig.Store(&cfg)

	a := app.NewWithID("SpaceDot")
	icon := fyne.NewStaticResource("icon.ico", iconData)
	a.SetIcon(icon)

	settings := newSettingsWindow(a)

	if desk, ok := a.(desktop.App); ok {
		quit := fyne.NewMenuItem("Выход", nil)
		// Иначе Fyne добавит в меню свой пункт «Выйти»
		quit.IsQuit = true

		desk.SetSystemTrayIcon(icon)
		desk.SetSystemTrayMenu(fyne.NewMenu("SpaceDot",
			fyne.NewMenuItem("Настройки", settings.show),
			fyne.NewMenuItemSeparator(),
			quit,
		))
	}

	a.Lifecycle().SetOnStarted(func() {
		// Fyne на Windows не задаёт подсказку трея — ставим только название
		systray.SetTooltip("SpaceDot")

		// ЛКМ по иконке → настройки; ПКМ по-прежнему открывает меню
		systray.SetOnTapped(func() { fyne.Do(settings.show) })

		whatsNewOnStart(a)
		go autoUpdateLoop(a)
	})
	a.Lifecycle().SetOnStopped(hook.End)
	go listenKeys()
	go cleanupOldExe()

	// a.Run блокирует главный поток до a.Quit()
	a.Run()
}

// ruleRow — блок настроек одной клавиши в окне
type ruleRow struct {
	key           uint16
	keyButton     *widget.Button
	doubleText    *widget.Entry
	tripleEnabled *widget.Check
	tripleText    *widget.Entry
	box           fyne.CanvasObject
}

func (r *ruleRow) setKey(vk uint16) {
	r.key = vk
	r.keyButton.SetText(keyName(vk))
	r.keyButton.Enable()
}

func (r *ruleRow) rule() Rule {
	return Rule{
		Key:           r.key,
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
}

func newSettingsWindow(a fyne.App) *settingsWindow {
	s := &settingsWindow{app: a, win: a.NewWindow("SpaceDot — настройки")}

	s.rowsBox = container.NewVBox()
	s.interval = widget.NewEntry()
	s.autostart = widget.NewCheck("Запускать вместе с Windows", nil)
	s.autoUpdate = widget.NewCheck("Обновлять автоматически", nil)
	s.updateButton = widget.NewButton("Проверить обновления", s.checkUpdateManually)

	addRule := widget.NewButtonWithIcon("Добавить клавишу", theme.ContentAddIcon(), func() {
		r := defaultRule()
		r.Key = 0
		r.DoubleText, r.TripleText = "", ""
		row := s.addRow(r)
		s.startCapture(row)
	})

	hint := widget.NewLabel("Пустое поле — ничего не заменять.")
	hint.Wrapping = fyne.TextWrapWord

	common := widget.NewForm(
		widget.NewFormItem("Интервал, мс", s.interval),
		widget.NewFormItem("", s.autostart),
		widget.NewFormItem("Версия "+version, s.autoUpdate),
		widget.NewFormItem("", s.updateButton),
	)

	save := widget.NewButton("Сохранить", s.save)
	save.Importance = widget.HighImportance

	top := container.NewVBox(widget.NewLabelWithStyle("Клавиши", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	bottom := container.NewVBox(
		addRule,
		widget.NewSeparator(),
		common,
		hint,
		container.NewHBox(widget.NewButton("По умолчанию", s.resetDefaults), save),
	)
	s.win.SetContent(container.NewPadded(container.NewBorder(top, bottom, nil, nil,
		container.NewVScroll(s.rowsBox))))
	s.win.Resize(fyne.NewSize(480, 600))
	// Закрытие окна только прячет его — программа продолжает работать в трее
	s.win.SetCloseIntercept(func() {
		s.cancelCapture()
		s.win.Hide()
	})

	return s
}

func (s *settingsWindow) addRow(rule Rule) *ruleRow {
	row := &ruleRow{
		doubleText: widget.NewEntry(),
		tripleText: widget.NewEntry(),
	}
	row.keyButton = widget.NewButton("", func() { s.startCapture(row) })
	row.tripleEnabled = widget.NewCheck("Тройное нажатие", func(on bool) {
		if on {
			row.tripleText.Enable()
		} else {
			row.tripleText.Disable()
		}
	})
	remove := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() { s.removeRow(row) })

	if rule.Key != 0 {
		row.setKey(rule.Key)
	}
	row.doubleText.SetText(rule.DoubleText)
	row.tripleText.SetText(rule.TripleText)
	row.tripleEnabled.SetChecked(rule.TripleEnabled)
	row.tripleEnabled.OnChanged(rule.TripleEnabled)

	form := widget.NewForm(
		widget.NewFormItem("Клавиша", container.NewBorder(nil, nil, nil, remove, row.keyButton)),
		widget.NewFormItem("Двойное нажатие →", row.doubleText),
		widget.NewFormItem("", row.tripleEnabled),
		widget.NewFormItem("Тройное нажатие →", row.tripleText),
	)
	row.box = widget.NewCard("", "", form)

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
	row.keyButton.SetText("Нажмите клавишу… (Esc — отмена)")
	row.keyButton.Disable()

	cb := func(vk uint16) {
		fyne.Do(func() {
			if s.capturing != row {
				return
			}
			s.capturing = nil
			if vk == 0 {
				vk = row.key
			}
			if vk == 0 {
				// новая строка, назначение отменено — убираем её
				s.removeRow(row)
				return
			}
			row.setKey(vk)
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
		row.setKey(row.key)
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
	seen := map[uint16]bool{}
	for _, row := range s.rows {
		if seen[row.key] {
			dialog.ShowError(errors.New("Клавиша «"+keyName(row.key)+"» назначена дважды"), s.win)
			return
		}
		seen[row.key] = true
		cfg.Rules = append(cfg.Rules, row.rule())
	}
	currentConfig.Store(&cfg)

	if err := saveConfig(cfg); err != nil {
		dialog.ShowError(err, s.win)
		return
	}
	if err := setAutostart(s.autostart.Checked); err != nil {
		dialog.ShowError(err, s.win)
	}
}
