package main

import (
	"time"
	"unicode/utf8"
)

type inputContext struct {
	window uintptr
	layout uint16
	app    string
}
type replacement struct {
	erase int
	text  string
}
type pressSequence struct {
	rule         Rule
	context      inputContext
	last         time.Time
	count, typed int
}

func (s *pressSequence) reset() { *s = pressSequence{} }
func (s *pressSequence) press(rule Rule, when time.Time, interval time.Duration, context inputContext) replacement {
	gap := when.Sub(s.last)
	if s.count == 0 || rule != s.rule || context != s.context || gap < 0 || gap >= interval {
		*s = pressSequence{rule: rule, context: context, last: when, count: 1, typed: 1}
		return replacement{}
	}
	s.last = when
	s.count++
	text := rule.DoubleText
	if s.count == 3 {
		text = rule.TripleText
	}
	action := replacement{}
	if text != "" {
		action = replacement{erase: s.typed + 1, text: text}
		s.typed = utf8.RuneCountInString(text)
	} else {
		s.typed++
	}
	if s.count == 3 || !rule.TripleEnabled {
		s.reset()
	}
	return action
}
