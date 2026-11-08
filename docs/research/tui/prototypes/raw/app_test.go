package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"kilntui/ledger"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name, in, events, rest string
	}{
		{"a lone Esc at the end of a read is the key", "\x1b", "[{esc 0 0 0}]", ""},
		{"an arrow", "\x1b[A", "[{up 0 0 0}]", ""},
		{"a press, and its release dropped", "\x1b[<0;3;5M\x1b[<0;3;5m", "[{ 1 2 4}]", ""},
		{"the wheel", "\x1b[<64;1;1Mk", "[{ 2 0 0} {k 0 0 0}]", ""},
		{"a report the read cut short waits for the rest", "j\x1b[<0;3", "[{j 0 0 0}]", "\x1b[<0;3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, rest := parse([]byte(tt.in))
			if got := fmt.Sprint(events); got != tt.events || string(rest) != tt.rest {
				t.Errorf("parse(%q) = %s, %q; want %s, %q", tt.in, got, rest, tt.events, tt.rest)
			}
		})
	}
}

// TestHandle drives the app at 100 by 30. There the table's rows start on row 2, and the chart's axis is row 28.
func TestHandle(t *testing.T) {
	keys := func(s ...string) []event {
		events := make([]event, len(s))
		for i, k := range s {
			events[i] = event{key: k}
		}

		return events
	}

	at := func(x, y int) event { return event{mouse: click, x: x, y: y} }
	tests := []struct {
		name   string
		events []event
		want   string
	}{
		{"keys open a build", keys("k", "k", "k", "k", "k", "enter"), "refused: A variable in this command"},
		{"a click selects", []event{at(10, 7)}, "gate-flakes · landed"},
		{"a second click opens", []event{at(10, 7), at(10, 7)}, "no failed calls"},
		{"a click on a bar selects", []event{at(13, 27)}, "amendments · stopped"},
		{"Esc goes back", keys("enter", "esc"), "cost per build"},
		{"the wheel stops at the oldest", slices.Repeat([]event{{mouse: wheelUp}}, 9), "status-line · accepted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newApp(ledger.Builds())
			a.w, a.h = 100, 30
			for _, e := range tt.events {
				a.handle(e)
			}

			if frame := a.draw().plain(); !strings.Contains(frame, tt.want) {
				t.Errorf("frame lacks %q:\n%s", tt.want, frame)
			}
		})
	}
}
