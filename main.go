package main

import (
	_ "embed"
	"time"

	"fyne.io/systray"
	"github.com/go-vgo/robotgo"
	hook "github.com/robotn/gohook"
)

//go:embed icon.ico
var iconData []byte

func main() {
	robotgo.KeySleep = 10

	// systray.Run блокирует главный поток до systray.Quit()
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("Double Space → .")
	systray.SetTooltip("Double Space → . + Space")

	mQuit := systray.AddMenuItem("Выход", "Закрыть программу")

	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()

	go listenKeys()
}

func onExit() {
	hook.End()
}

func listenKeys() {
	evChan := hook.Start()

	var lastSpaceTime time.Time
	var wasPreviousSpace bool

	for ev := range evChan {
		if ev.Kind != hook.KeyDown {
			continue
		}

		if ev.Rawcode != 32 { // не пробел
			// Другая клавиша → сброс
			wasPreviousSpace = false
			continue
		}

		now := ev.When

		// Проверяем, не слишком ли быстро после предыдущего KeyDown (удержание/автоповтор)
		if wasPreviousSpace && now.Sub(lastSpaceTime) < 500*time.Millisecond {
			// Если интервал очень маленький — вероятно удержание, пропускаем
			if now.Sub(lastSpaceTime) > 60*time.Millisecond { // ← настройка по предпочтениям (50–100 мс)
				// Удаляем два пробела
				robotgo.KeyTap("backspace")
				robotgo.MilliSleep(1)

				robotgo.KeyTap("backspace")
				robotgo.MilliSleep(1)

				// Вставляем ". " надёжно, независимо от раскладки
				robotgo.TypeStr(". ")

				wasPreviousSpace = false
				lastSpaceTime = time.Time{}
			} else {
				// Слишком быстро → вероятно удержание, просто обновляем время
				lastSpaceTime = now
			}
		} else {
			wasPreviousSpace = true
			lastSpaceTime = now
		}
	}
}
