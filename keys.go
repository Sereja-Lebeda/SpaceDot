package main

import (
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/go-vgo/robotgo"
	hook "github.com/robotn/gohook"
)

// Интервал короче этого — вероятно удержание/автоповтор, а не отдельное нажатие
const minGap = 60 * time.Millisecond

const vkEscape = 27

// Если не nil — следующее нажатие не обрабатывается, а передаётся сюда (назначение клавиши в GUI).
// Escape отменяет назначение и передаётся как 0.
var captureKey atomic.Pointer[func(vk uint16)]

func listenKeys() {
	evChan := hook.Start()

	var (
		lastPressTime time.Time
		seriesKey     uint16    // клавиша текущей серии
		count         int       // сколько нажатий подряд в текущей серии
		typed         int       // сколько символов серии сейчас стоит в тексте
		suppressUntil time.Time // события до этого момента — наш собственный ввод
	)

	// replace стирает символы серии вместе с символом от последнего нажатия и вставляет text
	replace := func(text string, perPress int) {
		for i := 0; i < typed+perPress; i++ {
			robotgo.KeyTap("backspace")
			robotgo.MilliSleep(1)
		}
		// Вставляем текст надёжно, независимо от раскладки
		robotgo.TypeStr(text)
		typed = utf8.RuneCountInString(text)
		suppressUntil = time.Now().Add(50 * time.Millisecond)
	}

	for ev := range evChan {
		if ev.Kind != hook.KeyDown {
			continue
		}
		// Наши же backspace и вставленный текст не должны сбрасывать серию
		if ev.When.Before(suppressUntil) {
			continue
		}

		if cb := captureKey.Swap(nil); cb != nil {
			vk := ev.Rawcode
			if vk == vkEscape {
				vk = 0
			}
			(*cb)(vk)
			count = 0
			continue
		}

		cfg := currentConfig.Load()
		rule := cfg.ruleFor(ev.Rawcode)
		if rule == nil {
			// Клавиша без правила → сброс
			count = 0
			continue
		}

		// Клавиши вроде пробела печатают символ, который тоже надо стереть; Shift, CapsLock и т.п. — нет
		perPress := 0
		if keyTypesChar(rule.Key) {
			perPress = 1
		}

		now := ev.When
		gap := now.Sub(lastPressTime)

		if count == 0 || ev.Rawcode != seriesKey || gap >= time.Duration(cfg.IntervalMs)*time.Millisecond {
			count, typed = 1, perPress
			seriesKey = ev.Rawcode
			lastPressTime = now
			continue
		}

		if gap <= minGap {
			// Слишком быстро → вероятно удержание, просто обновляем время
			lastPressTime = now
			continue
		}

		lastPressTime = now
		switch count {
		case 1:
			if rule.DoubleText != "" {
				replace(rule.DoubleText, perPress)
			} else {
				typed += perPress
			}
			count = 2
			if !rule.TripleEnabled {
				count = 0
			}
		case 2:
			if rule.TripleText != "" {
				replace(rule.TripleText, perPress)
			}
			count = 0
		}
	}
}
