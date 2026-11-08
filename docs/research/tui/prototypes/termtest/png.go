package termtest

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"

	uv "github.com/charmbracelet/ultraviolet"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// cellW and cellH are one cell's size in pixels, for a 16-pixel Go Mono.
const cellW, cellH = 10, 20

// mono is Go Mono, parsed once. A face made from it caches glyphs and is not safe to share, so each PNG makes its own.
var mono = func() *opentype.Font {
	f, err := opentype.Parse(gomono.TTF)
	if err != nil {
		panic(err)
	}

	return f
}()

// PNG writes the screen as an image with each cell's colors. An agent that reads images then sees what a person would.
// Go Mono lacks some glyphs a TUI draws. PNG fills a full block by hand, and draws other missing glyphs as a box.
func (term *Term) PNG(w io.Writer) error {
	face, err := opentype.NewFace(mono, &opentype.FaceOptions{Size: 16, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return err
	}

	img := image.NewRGBA(image.Rect(0, 0, term.emu.Width()*cellW, term.emu.Height()*cellH))
	draw.Draw(img, img.Bounds(), image.NewUniform(term.emu.BackgroundColor()), image.Point{}, draw.Src)
	for y := 0; y < term.emu.Height(); y++ {
		for x := 0; x < term.emu.Width(); x++ {
			if c := term.emu.CellAt(x, y); c != nil {
				term.drawCell(img, face, x, y, c)
			}
		}
	}

	return png.Encode(w, img)
}

func (term *Term) drawCell(img *image.RGBA, face font.Face, x, y int, c *uv.Cell) {
	fg, bg := color.Color(term.emu.ForegroundColor()), color.Color(term.emu.BackgroundColor())
	if c.Style.Fg != nil {
		fg = c.Style.Fg
	}

	if c.Style.Bg != nil {
		bg = c.Style.Bg
	}

	if c.Style.Attrs&uv.AttrReverse != 0 {
		fg, bg = bg, fg
	}

	cell := image.Rect(x*cellW, y*cellH, (x+max(c.Width, 1))*cellW, (y+1)*cellH)
	draw.Draw(img, cell, image.NewUniform(bg), image.Point{}, draw.Src)
	if c.Content == "█" {
		draw.Draw(img, cell, image.NewUniform(fg), image.Point{}, draw.Src)
		return
	}

	d := font.Drawer{Dst: img, Src: image.NewUniform(fg), Face: face,
		Dot: fixed.P(x*cellW, y*cellH+15)}
	d.DrawString(c.Content)
}
