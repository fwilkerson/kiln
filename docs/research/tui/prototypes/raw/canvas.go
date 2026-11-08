package main

import (
	"fmt"
	"strings"
)

// style is how a cell is drawn. Each holds one width-1 rune; a wide character would break the grid.
type style uint8

const (
	plain style = iota
	reverse
	accent
)

var sgr = [...]string{plain: "\x1b[0m", reverse: "\x1b[0;7m", accent: "\x1b[0;33m"}

type cell struct {
	r rune
	s style
}

// canvas is one frame, drawn in memory and written at once. The developer then never sees half a frame.
type canvas struct {
	w, h  int
	cells []cell
}

func newCanvas(w, h int) *canvas {
	c := &canvas{w: w, h: h, cells: make([]cell, w*h)}
	for i := range c.cells {
		c.cells[i].r = ' '
	}

	return c
}

// text writes s from column x of row y, clipped to the frame.
func (c *canvas) text(x, y int, s string, st style) {
	for _, r := range s {
		if x >= 0 && x < c.w && y >= 0 && y < c.h {
			c.cells[y*c.w+x] = cell{r, st}
		}

		x++
	}
}

// lines writes each line inside r's border, one per row, cut at its right edge.
func (c *canvas) lines(r rect, lines []string) {
	for i, line := range lines[:min(len(lines), max(r.h-2, 0))] {
		if runes := []rune(line); len(runes) > r.w-4 {
			line = string(runes[:max(r.w-4, 0)])
		}

		c.text(r.x+2, r.y+1+i, line, plain)
	}
}

// box draws r's border, with title in its top edge.
func (c *canvas) box(r rect, title string) {
	if r.w < 2 || r.h < 2 {
		return
	}

	edge := strings.Repeat("─", r.w-2)
	c.text(r.x, r.y, "┌"+edge+"┐", plain)
	c.text(r.x, r.y+r.h-1, "└"+edge+"┘", plain)
	for y := r.y + 1; y < r.y+r.h-1; y++ {
		c.text(r.x, y, "│", plain)
		c.text(r.x+r.w-1, y, "│", plain)
	}

	c.text(r.x+2, r.y, " "+title+" ", plain)
}

// String answers the frame as the escapes that draw it, each row placed absolutely so no write scrolls the screen.
func (c *canvas) String() string {
	var b strings.Builder
	for y := range c.h {
		fmt.Fprintf(&b, "\x1b[%d;1H", y+1)
		cur := style(255)
		for _, cl := range c.cells[y*c.w : (y+1)*c.w] {
			if cl.s != cur {
				b.WriteString(sgr[cl.s])
				cur = cl.s
			}

			b.WriteRune(cl.r)
		}
	}

	b.WriteString(sgr[plain])

	return b.String()
}

// plain answers the frame's text, one line per row, with trailing spaces dropped.
func (c *canvas) plain() string {
	rows := make([]string, c.h)
	for y := range rows {
		var line strings.Builder
		for _, cl := range c.cells[y*c.w : (y+1)*c.w] {
			line.WriteRune(cl.r)
		}

		rows[y] = strings.TrimRight(line.String(), " ")
	}

	return strings.Join(rows, "\n")
}
