package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
)

// theme is the view's styles, each named for what it marks. As on kiln's one-page sheet, a hue says who acts: the
// developer, an agent, or kiln. Red marks what failed, and green what passed. Text that marks nothing keeps the
// terminal's own color.
//
// Each style has a face for a dark terminal and one for a light one. The sheet's colors are the dark face. The light
// face darkens each hue until it reads on white. No one color reads well on both: the best reaches 4.4 to 1 on each.
type theme struct {
	// you marks the developer's part: the panel the keys go to, each key's name, and what waits on them.
	you lipgloss.Style
	// picked is the selected row, on a tint of you.
	picked lipgloss.Style
	// agent marks a Claude Code session: its role, and what runs.
	agent lipgloss.Style
	// kiln marks kiln's own code at work: a gate, a pause or a stop, and the trend's newest build.
	kiln           lipgloss.Style
	failed, passed lipgloss.Style
	// dim is what matters less than the text beside it. frame is a border the keys do not go to.
	dim, frame lipgloss.Style
	bold       lipgloss.Style
	// markdown colors a spec or an answer by its syntax, in hues that name no actor.
	markdown *chroma.Style
}

// The sheet's colors, and their light faces. Each comment gives its contrast on the sheet's canvas, #110b09, or on
// white. TestContrast holds them on lighter dark terminals, and darker light ones.
const (
	darkInk   = "#f6eadd" // 16.5
	darkYou   = "#ffe696" // 15.8
	darkAgent = "#8fbaff" // 9.9
	darkKiln  = "#ff8436" // 8.0
	// darkFailed is the sheet's cherry red, lightened: #e64b5e reaches only 4.4 on a #1e1e1e terminal.
	darkFailed = "#f06a78" // 6.5
	darkPassed = "#9ccf86" // 10.9
	darkDim    = "#ab9686" // 6.9
	// darkFrame is a warm gray that 256 colors draw as gray. The sheet's #7f5f4d fades on a #282c34 terminal.
	darkFrame = "#8c7a6e" // 4.8
	// darkPicked is a tint of you that 256 colors draw as gray. Warmer tints turn red or olive there.
	darkPicked = "#463d30" // 9.0, under ink

	lightInk   = "#2a1d17" // 16.3
	lightYou   = "#7d5a00" // 6.3
	lightAgent = "#2858a8" // 6.9
	lightKiln  = "#a84800" // 5.9
	// lightFailed stays red in 256 colors. A bluer red, such as #b8243a, turns gray there.
	lightFailed = "#b3202f" // 6.6
	lightPassed = "#2b7a3b" // 5.3
	lightDim    = "#75625a" // 5.8
	lightFrame  = "#9a8373" // 3.6
	lightPicked = "#f3e2b0" // 12.5, under ink
)

// newTheme is the theme for a terminal whose background is dark, or light.
func newTheme(dark bool) theme {
	face := lipgloss.LightDark(dark)
	fg := func(light, dark string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(face(lipgloss.Color(light), lipgloss.Color(dark)))
	}

	return theme{
		you: fg(lightYou, darkYou),
		picked: fg(lightInk, darkInk).Background(face(lipgloss.Color(lightPicked),
			lipgloss.Color(darkPicked))),
		agent:    fg(lightAgent, darkAgent),
		kiln:     fg(lightKiln, darkKiln),
		failed:   fg(lightFailed, darkFailed),
		passed:   fg(lightPassed, darkPassed),
		dim:      fg(lightDim, darkDim),
		frame:    fg(lightFrame, darkFrame),
		bold:     lipgloss.NewStyle().Bold(true),
		markdown: markdownStyle(face),
	}
}

// markdownStyle colors Markdown and the code in its fences. Its hues are muted, so none reads as an actor's. Prose and
// punctuation keep the terminal's own color, and no token is drawn on a background.
func markdownStyle(face func(light, dark color.Color) color.Color) *chroma.Style {
	hex := func(light, dark string) string {
		r, g, b, _ := face(lipgloss.Color(light), lipgloss.Color(dark)).RGBA()
		return chroma.NewColour(uint8(r>>8), uint8(g>>8), uint8(b>>8)).String()
	}

	tan, sage, slate := hex("#8a4a22", "#d69d74"), hex("#4d6420", "#b5c08c"), hex("#3c5a8a", "#a9b9d6")
	style, err := chroma.NewStyle("kiln", chroma.StyleEntries{
		chroma.GenericHeading:    "bold " + tan,
		chroma.GenericSubheading: "bold " + tan,
		chroma.GenericStrong:     "bold",
		chroma.GenericEmph:       "italic",
		chroma.Keyword:           tan,
		chroma.NameTag:           tan,
		chroma.NameEntity:        slate,
		chroma.LiteralString:     sage,
		chroma.LiteralNumber:     slate,
		chroma.Comment:           "italic " + hex(lightDim, darkDim),
	})
	if err != nil {
		panic(err)
	}

	return style
}
