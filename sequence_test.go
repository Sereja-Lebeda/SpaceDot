package main

import (
	"testing"
	"time"
)

func TestReplacementSequence(t *testing.T) {
	now := time.Now()
	ctx := inputContext{window: 1, layout: 0x419, app: "editor.exe"}
	r := Rule{Key: 190, Shift: true, DoubleText: ",", TripleEnabled: true, TripleText: "! "}
	var s pressSequence
	if a := s.press(r, now, time.Second, ctx); a.erase != 0 {
		t.Fatal(a)
	}
	if a := s.press(r, now.Add(10*time.Millisecond), time.Second, ctx); a.erase != 2 || a.text != "," {
		t.Fatal(a)
	}
	if a := s.press(r, now.Add(20*time.Millisecond), time.Second, ctx); a.erase != 2 || a.text != "! " {
		t.Fatal(a)
	}
	if a := s.press(r, now.Add(30*time.Millisecond), time.Second, ctx); a.erase != 0 {
		t.Fatal("series must reset after third press", a)
	}
}

func TestSequenceBoundaries(t *testing.T) {
	ctx := inputContext{window: 1, layout: 0x419}
	r := defaultRule()
	now := time.Now()
	for _, name := range []string{"timeout", "window", "layout", "modifier", "reset", "rule-change"} {
		t.Run(name, func(t *testing.T) {
			var s pressSequence
			s.press(r, now, 500*time.Millisecond, ctx)
			next, context, rule := now.Add(100*time.Millisecond), ctx, r
			switch name {
			case "timeout":
				next = now.Add(500 * time.Millisecond)
			case "window":
				context.window++
			case "layout":
				context.layout = 0x409
			case "modifier":
				rule.Shift = true
			case "reset":
				s.reset()
			case "rule-change":
				rule.DoubleText = ","
			}
			if a := s.press(rule, next, 500*time.Millisecond, context); a.erase != 0 {
				t.Fatal("crossed sequence boundary", a)
			}
		})
	}
}

func TestTripleAfterEmptyDouble(t *testing.T) {
	r := Rule{Key: 32, TripleEnabled: true, TripleText: "Привет"}
	now := time.Now()
	var s pressSequence
	s.press(r, now, time.Second, inputContext{})
	s.press(r, now.Add(time.Millisecond), time.Second, inputContext{})
	a := s.press(r, now.Add(2*time.Millisecond), time.Second, inputContext{})
	if a.erase != 3 || a.text != "Привет" {
		t.Fatal(a)
	}
}

func TestLayoutPriorityAndExclusions(t *testing.T) {
	cfg := Config{Rules: []Rule{
		{Key: 32, DoubleText: ". "},
		{Key: 32, Layout: 0x419, DoubleText: ", "},
		{Key: 32, Layout: 0x409, Disabled: true, DoubleText: "disabled"},
	}, ExcludedApps: []string{"Game.exe"}}
	if r := cfg.ruleForLayout(32, false, 0x419); r == nil || r.DoubleText != ", " {
		t.Fatal(r)
	}
	if r := cfg.ruleForLayout(32, false, 0x409); r == nil || r.DoubleText != ". " {
		t.Fatal(r)
	}
	if !cfg.excludes("game.EXE") || cfg.excludes("editor.exe") {
		t.Fatal("incorrect app exclusion")
	}
	cfg.Rules[0].Disabled = true
	if cfg.ruleForLayout(32, false, 0x409) != nil {
		t.Fatal("disabled rule matched")
	}
}
