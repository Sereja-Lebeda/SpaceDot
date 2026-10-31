package main

import (
	"testing"
	"time"
)

func TestKeyEventPipeline(t *testing.T) {
	for _, name := range []string{"shift", "repeat", "pause", "ctrl", "mouse", "navigation", "disabled", "excluded", "capture", "focus"} {
		t.Run(name, func(t *testing.T) {
			cfg := Config{IntervalMs: 500, Rules: []Rule{{Key: 190, Shift: true, DoubleText: ","}}}
			if name == "disabled" {
				cfg.Rules[0].Disabled = true
			}
			if name == "excluded" {
				cfg.ExcludedApps = []string{"editor.exe"}
			}
			currentConfig.Store(&cfg)
			paused.Store(name == "pause")
			defer paused.Store(false)
			captureKey.Store(nil)
			captured := uint16(0)
			if name == "capture" {
				cb := func(key uint16, shift bool) {
					captured = key
					if !shift {
						t.Error("Shift not captured")
					}
				}
				captureKey.Store(&cb)
				defer captureKey.Store(nil)
			}
			events := make(chan keyEvent, 12)
			now := time.Now()
			press := func(at time.Duration) {
				ev := keyEvent{key: 190, shift: true, blocked: name == "ctrl", window: 1, when: now.Add(at)}
				events <- ev
				if name == "repeat" {
					events <- ev
				}
				ev.up = true
				events <- ev
			}
			press(0)
			switch name {
			case "mouse":
				events <- keyEvent{reset: true}
			case "navigation":
				events <- keyEvent{key: 37}
				events <- keyEvent{key: 37, up: true}
			}
			press(100 * time.Millisecond)
			close(events)
			var actions []replacement
			context := inputContext{window: 1, layout: 0x419, app: "editor.exe"}
			if name == "focus" {
				context.window = 2
			}
			processKeyEvents(events, func() inputContext { return context }, func(n int, text string) error {
				actions = append(actions, replacement{n, text})
				return nil
			})
			if name == "shift" {
				if len(actions) != 1 || actions[0].erase != 2 || actions[0].text != "," {
					t.Fatal(actions)
				}
			} else if len(actions) != 0 {
				t.Fatal("unexpected replacement", actions)
			}
			if name == "capture" && captured != 190 {
				t.Fatal("capture failed", captured)
			}
		})
	}
}
