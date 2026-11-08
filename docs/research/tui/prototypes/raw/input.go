package main

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

// button is what a mouse report did, of the few the app reacts to. A release is dropped by parse.
type button int

const (
	none button = iota
	click
	wheelUp
	wheelDown
)

// event is one key, such as "up", "enter" or "q", or one mouse report. Its x and y count from 0.
type event struct {
	key   string
	mouse button
	x, y  int
}

// parse splits what the terminal sent into events. It answers the start of a sequence the read cut short, to put
// before the next read. A lone Esc at the end of a read is the Esc key, since a terminal writes a sequence whole.
func parse(b []byte) (events []event, rest []byte) {
	for len(b) > 0 {
		switch {
		case b[0] != 0x1b:
			r, n := utf8.DecodeRune(b)
			events = append(events, keyOf(r))
			b = b[n:]
		case len(b) == 1:
			return append(events, event{key: "esc"}), nil
		case b[1] != '[' && b[1] != 'O':
			// A terminal sends Alt and a key as Esc and the key. The app has no Alt keys, so it reads two keys.
			events = append(events, event{key: "esc"})
			b = b[1:]
		default:
			end := bytes.IndexFunc(b[2:], func(r rune) bool { return r >= 0x40 && r <= 0x7e })
			if end < 0 {
				return events, b
			}

			seq := b[:2+end+1]
			b = b[len(seq):]
			if e, ok := decode(seq); ok {
				events = append(events, e)
			}
		}
	}

	return events, nil
}

func keyOf(r rune) event {
	switch r {
	case '\r', '\n':
		return event{key: "enter"}
	case 3:
		return event{key: "ctrl+c"}
	}

	return event{key: string(r)}
}

// decode reads one escape sequence: an arrow key, or an SGR mouse press such as "\x1b[<0;12;5M".
func decode(seq []byte) (event, bool) {
	final, params := seq[len(seq)-1], string(seq[2:len(seq)-1])
	switch {
	case final == 'A':
		return event{key: "up"}, true
	case final == 'B':
		return event{key: "down"}, true
	case final != 'M' || !strings.HasPrefix(params, "<"):
		return event{}, false
	}

	var b, x, y int
	if _, err := fmt.Sscanf(params, "<%d;%d;%d", &b, &x, &y); err != nil {
		return event{}, false
	}

	e := event{x: x - 1, y: y - 1}
	// Bits 4, 8 and 16 hold Shift, Alt and Ctrl, which the app ignores.
	switch b &^ 0b11100 {
	case 0:
		e.mouse = click
	case 64:
		e.mouse = wheelUp
	case 65:
		e.mouse = wheelDown
	default:
		return event{}, false
	}

	return e, true
}
