package main

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// Как часто проверять обновления при включённом автообновлении
const autoUpdateEvery = 6 * time.Hour

func releaseNotes(r *release) fyne.CanvasObject {
	notes := r.Notes
	if notes == "" {
		notes = "Описание изменений отсутствует."
	}
	text := widget.NewRichTextFromMarkdown(notes)
	text.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(text)
	scroll.SetMinSize(fyne.NewSize(400, 220))
	return scroll
}

// showWhatsNew — плашка «Что нового» после обновления
func showWhatsNew(a fyne.App, r *release) {
	w := a.NewWindow("SpaceDot " + r.version() + " — что нового")
	ok := widget.NewButton("OK", w.Close)
	ok.Importance = widget.HighImportance
	w.SetContent(container.NewPadded(container.NewBorder(nil, container.NewHBox(ok), nil, nil, releaseNotes(r))))
	w.Resize(fyne.NewSize(440, 320))
	w.Show()
	w.RequestFocus()
}

// whatsNewOnStart показывает изменения, если программа запущена в новой версии впервые
func whatsNewOnStart(a fyne.App) {
	cfg := *currentConfig.Load()
	if cfg.LastSeenVersion == version {
		return
	}
	firstRun := cfg.LastSeenVersion == ""
	cfg.LastSeenVersion = version
	currentConfig.Store(&cfg)
	_ = saveConfig(cfg)

	if firstRun {
		return
	}
	go func() {
		r, err := releaseByVersion(version)
		if err != nil {
			return
		}
		fyne.Do(func() { showWhatsNew(a, r) })
	}()
}

// applyUpdate ставит обновление и перезапускает программу
func applyUpdate(a fyne.App, r *release) error {
	if err := installUpdate(r); err != nil {
		return err
	}
	if err := restart(); err != nil {
		return err
	}
	fyne.Do(a.Quit)
	return nil
}

// autoUpdateLoop периодически проверяет GitHub и, если включено автообновление, обновляется сам
func autoUpdateLoop(a fyne.App) {
	time.Sleep(10 * time.Second)
	for {
		if currentConfig.Load().AutoUpdate {
			if r, err := checkUpdate(); err == nil && r != nil {
				if applyUpdate(a, r) == nil {
					return
				}
			}
		}
		time.Sleep(autoUpdateEvery)
	}
}

// checkUpdateManually — кнопка «Проверить обновления» в настройках
func (s *settingsWindow) checkUpdateManually() {
	s.updateButton.Disable()
	s.updateButton.SetText("Проверка…")

	go func() {
		r, err := checkUpdate()
		fyne.Do(func() {
			s.updateButton.Enable()
			s.updateButton.SetText("Проверить обновления")

			switch {
			case err != nil:
				dialog.ShowError(err, s.win)
			case r == nil:
				dialog.ShowInformation("Обновления", "У вас последняя версия ("+version+")", s.win)
			default:
				d := dialog.NewCustomConfirm("Доступна версия "+r.version(), "Обновить", "Позже",
					releaseNotes(r), func(ok bool) {
						if ok {
							s.installManually(r)
						}
					}, s.win)
				d.Show()
			}
		})
	}()
}

func (s *settingsWindow) installManually(r *release) {
	s.updateButton.Disable()
	s.updateButton.SetText("Загрузка…")
	go func() {
		err := applyUpdate(s.app, r)
		if err != nil {
			fyne.Do(func() {
				s.updateButton.Enable()
				s.updateButton.SetText("Проверить обновления")
				dialog.ShowError(err, s.win)
			})
		}
	}()
}
