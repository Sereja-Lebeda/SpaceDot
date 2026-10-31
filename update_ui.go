package main

import (
	"errors"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const updateCheckEvery = 6 * time.Hour

var errUpdateBusy = errors.New("проверка или установка обновления уже выполняется")

func releaseNotes(r *release) fyne.CanvasObject {
	notes := r.Notes
	if notes == "" {
		notes = "Описание изменений отсутствует."
	}
	text := widget.NewRichTextFromMarkdown(notes)
	text.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(text)
	scroll.SetMinSize(fyne.NewSize(560, 380))
	return scroll
}

func (s *settingsWindow) showWhatsNew(r *release) {
	s.show()
	d := dialog.NewCustom("Что нового в версии "+r.version(), "Понятно", releaseNotes(r), s.win)
	d.Resize(fyne.NewSize(640, 540))
	d.Show()
}

func (s *settingsWindow) whatsNewOnStart() {
	cfg := *currentConfig.Load()
	if cfg.LastSeenVersion == version {
		return
	}
	firstRun := cfg.LastSeenVersion == ""
	if firstRun {
		cfg.LastSeenVersion = version
		if saveConfig(cfg) == nil {
			currentConfig.Store(&cfg)
		}
		return
	}
	go func() {
		r, err := releaseByVersion(version)
		if err != nil {
			return
		}
		fyne.Do(func() {
			s.showWhatsNew(r)
			// Не отмечаем описание прочитанным, если получить его не удалось.
			cfg := *currentConfig.Load()
			cfg.LastSeenVersion = version
			if saveConfig(cfg) == nil {
				currentConfig.Store(&cfg)
			}
		})
	}()
}

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

func (s *settingsWindow) requestUpdateCheck() {
	select {
	case s.updateWake <- struct{}{}:
	default:
	}
}

func (s *settingsWindow) updateLoop() {
	runUpdateSchedule(s.updateStop, s.updateWake, 10*time.Second, updateCheckEvery, updateRetryDelay, func() bool {
		err := s.checkForUpdates(false)
		return err != nil
	})
}

// Один флаг защищает фоновые и ручные проверки и установку от одновременного запуска.
func (s *settingsWindow) checkForUpdates(manual bool) error {
	if !s.updateBusy.CompareAndSwap(false, true) {
		return errUpdateBusy
	}
	defer s.updateBusy.Store(false)
	fyne.Do(func() {
		s.updateButton.Disable()
		s.updateButton.SetText("Проверка…")
		s.updateStatus.SetText("Проверяем наличие обновлений…")
	})
	r, err := checkUpdate()
	if err != nil {
		s.reportUpdateError(err, manual)
		return err
	}
	if r == nil {
		fyne.Do(func() {
			s.update = nil
			s.resetUpdateButton()
			s.updateStatus.SetText("Проверено в " + time.Now().Format("15:04") + " · установлена последняя версия " + version)
			if manual {
				dialog.ShowInformation("Обновления", "У вас последняя версия ("+version+")", s.win)
			}
		})
		return nil
	}
	if !manual && currentConfig.Load().AutoUpdate {
		fyne.Do(func() { s.update = r })
		if err = s.performInstall(r); err != nil {
			s.reportUpdateError(err, false)
			return err
		}
		return nil
	}
	fyne.Do(func() {
		s.setUpdateAvailable(r)
		s.updateStatus.SetText("Доступна версия " + r.version())
		if manual {
			s.confirmUpdate(r)
		}
	})
	return nil
}

func (s *settingsWindow) reportUpdateError(err error, manual bool) {
	fyne.Do(func() {
		s.resetUpdateButton()
		s.updateStatus.SetText("Не удалось обновиться: " + err.Error() + ". Повторная проверка — через 1–15 минут.")
		if manual {
			dialog.ShowError(err, s.win)
		}
	})
}

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

func (s *settingsWindow) onUpdateButton() {
	if s.updateBusy.Load() {
		return
	}
	if s.update != nil {
		s.confirmUpdate(s.update)
		return
	}
	go func() {
		if err := s.checkForUpdates(true); err != nil {
			s.requestUpdateCheck()
		}
	}()
}

func (s *settingsWindow) confirmUpdate(r *release) {
	d := dialog.NewCustomConfirm("Доступна версия "+r.version(), "Обновить", "Позже", releaseNotes(r), func(ok bool) {
		if ok {
			s.installManually(r)
		}
	}, s.win)
	d.Resize(fyne.NewSize(640, 540))
	d.Show()
}

// Вызывается только при захваченном updateBusy.
func (s *settingsWindow) performInstall(r *release) error {
	fyne.Do(func() {
		s.updateButton.Disable()
		s.updateButton.SetText("Загрузка…")
		s.updateStatus.SetText("Скачиваем версию " + r.version() + ". После установки SpaceDot перезапустится автоматически.")
	})
	var lastPercent int64 = -1
	progress := func(done, total int64) {
		if total <= 0 {
			return
		}
		percent := done * 100 / total
		if percent == lastPercent {
			return
		}
		lastPercent = percent
		fyne.Do(func() { s.updateButton.SetText(fmt.Sprintf("Загрузка… %d%%", percent)) })
	}
	return applyUpdate(s.app, r, progress)
}

func (s *settingsWindow) installManually(r *release) {
	if !s.updateBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.updateBusy.Store(false)
		if err := s.performInstall(r); err != nil {
			s.reportUpdateError(err, true)
			s.requestUpdateCheck()
		}
	}()
}
