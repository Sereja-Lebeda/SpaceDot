package main

import (
	"sync/atomic"
	"time"
)

const vkEscape = 27

var captureKey atomic.Pointer[func(vk uint16, shift bool)]

// Замена на отпускании: исходный символ уже доставлен приложению.
func listenKeys(events <-chan keyEvent) {
	processKeyEvents(events, foregroundContext, replaceText)
}

func processKeyEvents(events <-chan keyEvent, contextForInput func() inputContext, apply func(int, string) error) {
	var sequence pressSequence
	var previousConfig *Config
	var down [256]*keyEvent
	for ev := range events {
		if ev.reset {
			sequence.reset()
			for _, press := range down {
				if press != nil {
					press.repeat = true
				}
			}
			continue
		}
		if ev.key >= 256 {
			continue
		}
		if !ev.up {
			if down[ev.key] != nil {
				down[ev.key].repeat = true
				sequence.reset()
			} else {
				copy := ev
				down[ev.key] = &copy
			}
			if cb := captureKey.Load(); cb != nil {
				sequence.reset()
				if isModifier(ev.key) || (ev.key != vkEscape && (!keyTypesChar(ev.key) || ev.blocked)) {
					continue
				}
				if captureKey.CompareAndSwap(cb, nil) {
					key := ev.key
					if key == vkEscape {
						key = 0
					}
					(*cb)(key, ev.shift)
				}
				down[ev.key].repeat = true
				continue
			}
			if ev.blocked || (isModifier(ev.key) && ev.key != 0x10 && ev.key != 0xA0 && ev.key != 0xA1) || (!isModifier(ev.key) && !keyTypesChar(ev.key)) {
				sequence.reset()
			}
			if !isModifier(ev.key) && sequence.count != 0 && (ev.key != sequence.rule.Key || ev.shift != sequence.rule.Shift) {
				sequence.reset()
			}
			continue
		}
		press := down[ev.key]
		down[ev.key] = nil
		if press == nil || isModifier(ev.key) {
			continue
		}
		cfg := currentConfig.Load()
		if cfg != previousConfig {
			sequence.reset()
			previousConfig = cfg
		}
		context := contextForInput()
		if press.repeat || paused.Load() || ev.blocked || press.blocked || context.window == 0 || context.window != press.window || cfg.excludes(context.app) {
			sequence.reset()
			continue
		}
		rule := cfg.ruleForLayout(ev.key, press.shift, context.layout)
		if rule == nil || !keyTypesChar(ev.key) {
			sequence.reset()
			continue
		}
		action := sequence.press(*rule, press.when, time.Duration(cfg.IntervalMs)*time.Millisecond, context)
		if action.erase == 0 {
			continue
		}
		if contextForInput() != context {
			sequence.reset()
			continue
		}
		if err := apply(action.erase, action.text); err != nil {
			sequence.reset()
		}
	}
}
