// Command raw draws kiln's build ledger in a terminal with no TUI library, as the baseline a library must beat.
//
//	builds            a table, one row per build; up, down, k, j or the wheel move the selection
//	cost per build    a bar chart; a click on a bar selects its build
//	selected          the selected build; Enter, or a click on the selected row, opens it, and Esc goes back
//
// It puts the terminal in raw mode, draws on the alternate screen, and asks for SGR mouse reports. Each frame redraws
// every cell, so a resize needs no clear.
package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"kilntui/ledger"
)

const (
	// setup enters the alternate screen, hides the cursor, and asks for button and SGR mouse reports.
	setup   = "\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h"
	restore = "\x1b[?1006l\x1b[?1000l\x1b[?25h\x1b[?1049l"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	old, err := term.MakeRaw(in)
	if err != nil {
		return err
	}

	// A deferred call runs while a panic in this goroutine unwinds, so the shell is not left raw and blank.
	_, _ = os.Stdout.WriteString(setup)
	defer func() {
		_, _ = os.Stdout.WriteString(restore)
		_ = term.Restore(in, old)
	}()

	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	input := make(chan []byte)
	go read(os.Stdin, input)

	a := newApp(ledger.Builds())
	var rest []byte
	for {
		if a.w, a.h, err = term.GetSize(out); err != nil {
			return err
		}

		if _, err := os.Stdout.WriteString(a.draw().String()); err != nil {
			return err
		}

		select {
		case <-resized:
		case b, ok := <-input:
			if !ok {
				return nil
			}

			var events []event
			events, rest = parse(append(rest, b...))
			for _, e := range events {
				if a.handle(e) {
					return nil
				}
			}
		}
	}
}

// read sends each read from r, so the loop can wait on input and a resize at once. It closes to when r ends.
func read(r io.Reader, to chan<- []byte) {
	for {
		b := make([]byte, 256)
		n, err := r.Read(b)
		if err != nil {
			close(to)
			return
		}

		to <- b[:n]
	}
}
