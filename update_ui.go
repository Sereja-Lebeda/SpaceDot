package main

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Как часто проверять обновления в фоне
const updateCheckEvery = 6 * time.Hour

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

// showWhatsNew открывает настройки и показывает в них модальную плашку «Что нового»;
// после её закрытия окно настроек остаётся открытым
func (s *settingsWindow) showWhatsNew(r *release) {
	s.show()
	dialog.NewCustom("Что нового в версии "+r.version(), "OK", releaseNotes(r), s.win).Show()
}

// whatsNewOnStart показывает изменения, если программа запущена в новой версии впервые
func (s *settingsWindow) whatsNewOnStart() {
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
		fyne.Do(func() { s.showWhatsNew(r) })
	}()
}

// applyUpdate ставит обновление и перезапускает программу
func applyUpdate(a fyne.App, r *release, progress func(done, total int64)) error {
	if err := installUpdate(r, progress); err != nil {
		return err
	}
	if err := restart(); err != nil {
		return err
	}
	fyne.Do(a.Quit)
	return nil
}

// updateLoop периодически проверяет GitHub: при автообновлении ставит новую версию сам,
// иначе подсвечивает кнопку обновления в настройках
func (s *settingsWindow) updateLoop() {
	time.Sleep(10 * time.Second)
	for {
		if r, err := checkUpdate(); err == nil && r != nil {
			if currentConfig.Load().AutoUpdate && applyUpdate(s.app, r, nil) == nil {
				return
			}
			fyne.Do(func() { s.setUpdateAvailable(r) })
		}
		time.Sleep(updateCheckEvery)
	}
}

// setUpdateAvailable переключает кнопку в режим «есть обновление»
func (s *settingsWindow) setUpdateAvailable(r *release) {
	s.update = r
	s.resetUpdateButton()
}

func (s *settingsWindow) resetUpdateButton() {
	b := s.updateButton
	if s.update != nil {
		b.SetText("Обновить до " + s.update.version())
		b.SetIcon(theme.DownloadIcon())
		b.Importance = widget.SuccessImportance
	} else {
		b.SetText("Проверить обновления")
		b.SetIcon(nil)
		b.Importance = widget.MediumImportance
	}
	b.Enable()
	b.Refresh()
}

// onUpdateButton — если обновление уже найдено, предлагает его поставить, иначе проверяет GitHub
func (s *settingsWindow) onUpdateButton() {
	if s.update != nil {
		s.confirmUpdate(s.update)
		return
	}

	s.updateButton.Disable()
	s.updateButton.SetText("Проверка…")
	go func() {
		r, err := checkUpdate()
		fyne.Do(func() {
			s.resetUpdateButton()
			switch {
			case err != nil:
				dialog.ShowError(err, s.win)
			case r == nil:
				dialog.ShowInformation("Обновления", "У вас последняя версия ("+version+")", s.win)
			default:
				s.setUpdateAvailable(r)
				s.confirmUpdate(r)
			}
		})
	}()
}

func (s *settingsWindow) confirmUpdate(r *release) {
	dialog.NewCustomConfirm("Доступна версия "+r.version(), "Обновить", "Позже",
		releaseNotes(r), func(ok bool) {
			if ok {
				s.installManually(r)
			}
		}, s.win).Show()
}

func (s *settingsWindow) installManually(r *release) {
	s.updateButton.Disable()
	s.updateButton.SetText("Загрузка…")

	var lastPercent int64 = -1
	progress := func(done, total int64) {
		if total <= 0 {
			return
		}
		// Обновляем текст только при смене процента, чтобы не заваливать UI
		percent := done * 100 / total
		if percent == lastPercent {
			return
		}
		lastPercent = percent
		fyne.Do(func() { s.updateButton.SetText(fmt.Sprintf("Загрузка… %d%%", percent)) })
	}

	go func() {
		if err := applyUpdate(s.app, r, progress); err != nil {
			fyne.Do(func() {
				s.resetUpdateButton()
				dialog.ShowError(err, s.win)
			})
		}
	}()
}
