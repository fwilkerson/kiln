package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// command is a key binding, and what it does on the top screen.
type command struct {
	key.Binding
	do func(m *model, s *screen) tea.Cmd
}

// keyMap is what one kind of screen takes. It both handles each key and names it. So the footer and the key list
// name only keys the screen takes, and the list names each one.
type keyMap struct {
	// commands are every binding the screen takes, in the order the key list names them.
	commands []command
	// foot are the bindings the footer names, a pair of them as one.
	foot []key.Binding
}

var (
	quit     = key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit"))
	back     = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	keysList = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "keys"))
	spec     = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "spec as approved"))
)

// keyMaps holds each kind of screen's keys.
var keyMaps = map[kind]keyMap{
	homeScreen:  homeKeys(),
	specScreen:  listKeys("build"),
	buildScreen: listKeys("run"),
	runScreen:   textKeys(true),
	textScreen:  textKeys(false),
}

func homeKeys() keyMap {
	down := key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "down"))
	up := key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k", "up"))
	tab := key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "panel"))
	filter := key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter"))
	open := key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open"))
	return keyMap{
		commands: []command{
			{down, func(m *model, _ *screen) tea.Cmd { m.move(1); return nil }},
			{up, func(m *model, _ *screen) tea.Cmd { m.move(-1); return nil }},
			{tab, func(m *model, _ *screen) tea.Cmd { m.focus = m.nextPane(1); return nil }},
			{key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "panel before")),
				func(m *model, _ *screen) tea.Cmd { m.focus = m.nextPane(-1); return nil }},
			{filter, func(m *model, _ *screen) tea.Cmd {
				m.typing, m.focus, m.sel[specsPane] = true, specsPane, 0
				return nil
			}},
			{open, func(m *model, _ *screen) tea.Cmd {
				return m.openTarget(target{paneTargets[m.focus], m.sel[m.focus]})
			}},
			{key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear the filter")),
				func(m *model, _ *screen) tea.Cmd { m.filter = ""; return nil }},
			showKeys, quits,
		},
		foot: []key.Binding{pair(down, up, "select"), tab, filter, open, keysList, quit},
	}
}

// listKeys are the keys of a screen that lists what opens, a spec's builds or a build's runs. Each moves the
// selection, but space and b, which scroll the list a page.
func listKeys(opens string) keyMap {
	down := key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "down"))
	up := key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k", "up"))
	pageDown := key.NewBinding(key.WithKeys("space", "pgdown"), key.WithHelp("space", "page down"))
	pageUp := key.NewBinding(key.WithKeys("b", "pgup"), key.WithHelp("b", "page up"))
	first := key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "first"))
	last := key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "last"))
	open := key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", opens))
	return keyMap{
		commands: []command{
			{down, func(m *model, s *screen) tea.Cmd { m.selectRow(s, m.row(s)+1); return nil }},
			{up, func(m *model, s *screen) tea.Cmd { m.selectRow(s, m.row(s)-1); return nil }},
			{pageDown, scroll((*viewport.Model).PageDown)},
			{pageUp, scroll((*viewport.Model).PageUp)},
			{first, func(m *model, s *screen) tea.Cmd { m.selectRow(s, 0); return nil }},
			{last, func(m *model, s *screen) tea.Cmd { m.selectRow(s, m.rows(s)-1); return nil }},
			{open, func(m *model, s *screen) tea.Cmd {
				if s.kind == specScreen {
					return m.openTarget(target{toBuild, s.sel})
				}

				return m.openTarget(target{toRun, s.sel})
			}},
			{spec, func(m *model, s *screen) tea.Cmd {
				if s.kind == specScreen {
					return m.openText(s.spec, s.sel)
				}

				return m.openText(s.spec, s.build)
			}},
			goesBack, showKeys, quits,
		},
		foot: []key.Binding{pair(down, up, "select"), pair(pageDown, pageUp, "page"), pair(first, last, "first/last"),
			open, spec, keysList, back},
	}
}

// textKeys are the keys of a screen that scrolls, a run's or the spec text. Its scrolling keys are the viewport's
// own. A run's screen opens its spec text too.
func textKeys(run bool) keyMap {
	vp := viewport.DefaultKeyMap()
	vp.Down.SetHelp("j", "down")
	vp.Up.SetHelp("k", "up")
	vp.PageDown.SetHelp("space", "page down")
	vp.PageUp.SetHelp("b", "page up")
	vp.HalfPageDown.SetHelp("d", "half a page down")
	vp.HalfPageUp.SetHelp("u", "half a page up")
	vp.Left.SetHelp("h", "left")
	vp.Right.SetHelp("l", "right")
	top := key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top"))
	end := key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "end"))
	commands := []command{
		{vp.Down, scroll(func(v *viewport.Model) { v.ScrollDown(1) })},
		{vp.Up, scroll(func(v *viewport.Model) { v.ScrollUp(1) })},
		{vp.PageDown, scroll((*viewport.Model).PageDown)},
		{vp.PageUp, scroll((*viewport.Model).PageUp)},
		{vp.HalfPageDown, scroll((*viewport.Model).HalfPageDown)},
		{vp.HalfPageUp, scroll((*viewport.Model).HalfPageUp)},
		{vp.Left, scroll(func(v *viewport.Model) { v.ScrollLeft(sideways) })},
		{vp.Right, scroll(func(v *viewport.Model) { v.ScrollRight(sideways) })},
		{top, scroll(func(v *viewport.Model) { v.GotoTop() })},
		{end, scroll(func(v *viewport.Model) { v.GotoBottom() })},
	}
	foot := []key.Binding{pair(vp.Down, vp.Up, "scroll"), pair(vp.PageDown, vp.PageUp, "page"),
		pair(top, end, "top/end")}
	if run {
		commands = append(commands, command{spec, func(m *model, s *screen) tea.Cmd {
			return m.openText(s.spec, s.build)
		}})

		foot = append(foot, spec)
	} else {
		foot = append(foot, pair(vp.Left, vp.Right, "sideways"))
	}

	return keyMap{commands: append(commands, goesBack, showKeys, quits), foot: append(foot, keysList, back)}
}

// sideways is how many columns h and l scroll a text screen.
const sideways = 6

var (
	goesBack = command{back, func(m *model, _ *screen) tea.Cmd {
		m.stack = m.stack[:len(m.stack)-1]
		return nil
	}}
	showKeys = command{keysList, func(m *model, _ *screen) tea.Cmd { m.keysShown = true; return nil }}
	quits    = command{quit, func(*model, *screen) tea.Cmd { return tea.Quit }}
)

func scroll(f func(*viewport.Model)) func(*model, *screen) tea.Cmd {
	return func(_ *model, s *screen) tea.Cmd {
		f(&s.body)
		return nil
	}
}

// pair names two bindings in the footer as one, such as j/k, and takes the keys of both.
func pair(a, b key.Binding, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(append(a.Keys(), b.Keys()...)...),
		key.WithHelp(a.Help().Key+"/"+b.Help().Key, desc))
}

// do carries out the command the key names on the top screen, if the screen takes it.
func (m *model) do(k tea.KeyPressMsg) tea.Cmd {
	s := m.top()
	for _, c := range keyMaps[s.kind].commands {
		if key.Matches(k, c.Binding) {
			return c.do(m, s)
		}
	}

	return nil
}

// footer names the keys the screen's footer names, in at most w cells. It names whole bindings only. Where all do not
// fit, ? and esc come first, then the rest in order, and the key list names the ones left out.
func footer(k kind, w int) string {
	foot := keyMaps[k].foot
	first := func(b key.Binding) bool {
		return b.Help().Key == keysList.Help().Key || b.Help().Key == back.Help().Key
	}

	order := append(slices.DeleteFunc(slices.Clone(foot), func(b key.Binding) bool { return !first(b) }),
		slices.DeleteFunc(slices.Clone(foot), first)...)
	kept, used := map[string]bool{}, 0
	for _, b := range order {
		named := b.Help().Key + " " + b.Help().Desc
		if need := ansi.StringWidth(named) + 3*min(1, used); used+need <= w {
			kept[named], used = true, used+need
		}
	}

	var named []string
	for _, b := range foot {
		if n := b.Help().Key + " " + b.Help().Desc; kept[n] {
			named = append(named, n)
		}
	}

	return strings.Join(named, " · ")
}

// keyNames shows a key as the key list names it, where its name is not the key list's.
var keyNames = map[string]string{"up": "↑", "down": "↓", "left": "←", "right": "→", "pgdown": "pgdn"}

// keyList is a box that lists every key the screen of kind k takes, and what each does.
func (m model) keyList(k kind) *lipgloss.Layer {
	type line struct{ keys, desc string }
	var lines []line
	for _, c := range keyMaps[k].commands {
		keys := slices.Clone(c.Keys())
		// A letter first, then a key's name, as the footer names a letter.
		slices.SortStableFunc(keys, func(a, b string) int { return cmp.Compare(len(a), len(b)) })
		for i, k := range keys {
			keys[i] = cmp.Or(keyNames[k], k)
		}

		shown := strings.Join(keys, " ")
		lines = append(lines, line{shown, c.Help().Desc})
	}

	// A list taller than the terminal goes into columns, so the box always fits its height.
	perColumn := max(1, min(len(lines), m.h-4))
	var columns []string
	for chunk := range slices.Chunk(lines, perColumn) {
		keysW := 0
		for _, l := range chunk {
			keysW = max(keysW, ansi.StringWidth(l.keys))
		}

		var rows []string
		for _, l := range chunk {
			rows = append(rows, fmt.Sprintf("%-*s  %s", keysW, l.keys, dim.Render(l.desc)))
		}

		columns = append(columns, strings.Join(rows, "\n"), "   ")
	}

	body := []string{bold.Render("keys"), lipgloss.JoinHorizontal(lipgloss.Top, columns[:len(columns)-1]...)}
	if len(lines)+5 <= m.h {
		body = append(body, "")
	}

	body = append(body, dim.Render("? or esc closes this"))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.BrightBlack).
		Padding(0, 1).Render(strings.Join(body, "\n"))
	x := max(0, (m.w-lipgloss.Width(box))/2)
	y := max(0, (m.h-lipgloss.Height(box))/2)
	return lipgloss.NewLayer(box).X(x).Y(y).Z(keyListZ)
}

// keyListZ puts the key list above every layer of a screen.
const keyListZ = 10
