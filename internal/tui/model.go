package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"

	"github.com/fwilkerson/kiln/internal/claude"
)

// model is the whole view's state. Bubble Tea passes it by value, and Update answers the changed copy.
type model struct {
	src Source
	// every is how often the view reads its source again, or 0 for never.
	every time.Duration
	specs []Spec
	err   error
	// now is when the source was last read. Every time shown is counted to it.
	now time.Time
	// live is the running run's stretch of its stream, for the now line.
	live *claude.Recounted
	w, h int
	// stack holds the screens the developer opened, home first. Esc takes the top one off.
	stack []*screen
	// open names the spec to open once the source is first read.
	open string

	// The home screen's.
	focus  pane
	filter string
	typing bool
	sel    [panes]int
}

// pane is a part of the home screen that tab moves the keys to.
type pane int

const (
	specsPane pane = iota
	nowPane
	recentPane
	panes
)

type kind int

const (
	homeScreen kind = iota
	specScreen
	buildScreen
	runScreen
	textScreen
)

// screen is one screen the developer opened. Its spec, build and run say what it shows, as far as its kind needs.
type screen struct {
	kind  kind
	spec  string
	build int
	run   int
	// sel is the selected build, on a spec's screen, or run, on a build's.
	sel int
	// reveal brings the selection into view at the next refresh.
	reveal bool
	body   viewport.Model
	// follow keeps a run's newest call in view, until the developer scrolls up.
	follow bool
	calls  *claude.Recounted
	// text is the spec as approved, as Markdown, and drawn is it drawn for a width of drawnW.
	text   string
	drawn  []string
	drawnW int
}

func newModel(src Source, name string, every time.Duration) model {
	return model{src: src, every: every, open: name, w: 80, h: 24, stack: []*screen{{kind: homeScreen}}}
}

// tick asks the view to read its source again.
type tick struct{}

// loaded is what one read of the source found.
type loaded struct {
	at    time.Time
	specs []Spec
	err   error
	live  *claude.Recounted
	// opened is the run the top screen showed when the read began, and open its stretch of the stream.
	opened runRef
	open   *claude.Recounted
}

// texted is a build's spec as approved, read.
type texted struct {
	spec  string
	build int
	text  string
	err   error
}

// runRef names a run by its spec, its build's index, and its index among the build's runs.
type runRef struct {
	spec       string
	build, run int
}

func (m model) Init() tea.Cmd { return tea.Batch(m.load(), m.tick()) }

func (m model) tick() tea.Cmd {
	if m.every == 0 {
		return nil
	}

	return tea.Tick(m.every, func(time.Time) tea.Msg { return tick{} })
}

// load reads the source, the running run's stream, and the stream of the run the top screen shows.
func (m model) load() tea.Cmd {
	src, opened, open := m.src, runRef{}, false
	if s := m.top(); s.kind == runScreen {
		// An ended run's stream no longer grows, so it is read once.
		r := find(m.specs, runRef{s.spec, s.build, s.run})
		opened, open = runRef{s.spec, s.build, s.run}, s.calls == nil || r == nil || r.running()
	}

	return func() tea.Msg {
		specs, err := src.Specs(context.Background())
		msg := loaded{at: time.Now(), specs: specs, err: err, opened: opened}
		if _, r, ok := running(specs); ok {
			if counted, err := src.Recount(r); err == nil {
				msg.live = &counted
			}
		}

		if r := find(specs, opened); open && r != nil {
			if counted, err := src.Recount(r); err == nil {
				msg.open = &counted
			}
		}

		return msg
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tick:
		cmd = tea.Batch(m.load(), m.tick())
	case loaded:
		m.loaded(msg)
	case texted:
		if s := m.top(); s.kind == textScreen && s.spec == msg.spec && s.build == msg.build {
			s.text = msg.text
			if msg.err != nil {
				s.text = "kiln could not read the spec: " + msg.err.Error()
			}
		}
	case tea.KeyPressMsg:
		cmd = m.key(msg)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			t, _ := parseTarget(m.layout().Hit(msg.X, msg.Y).ID())
			cmd = m.openTarget(t)
		}
	case tea.MouseWheelMsg:
		m.wheel(msg.Button == tea.MouseWheelDown)
	}

	m.refresh()
	return m, cmd
}

func (m *model) loaded(msg loaded) {
	m.now, m.err, m.live = msg.at, msg.err, msg.live
	if msg.err == nil {
		m.specs = msg.specs
	}

	if s := m.top(); s.kind == runScreen && msg.open != nil && msg.opened == (runRef{s.spec, s.build, s.run}) {
		s.calls = msg.open
	}

	if m.open != "" {
		m.push(&screen{kind: specScreen, spec: m.open, sel: max(0, len(m.builds(m.open))-1), reveal: true})
		m.open = ""
	}
}

func (m model) top() *screen { return m.stack[len(m.stack)-1] }

func (m *model) push(s *screen) {
	s.body = viewport.New()
	m.stack = append(m.stack, s)
}

func (m *model) key(k tea.KeyPressMsg) tea.Cmd {
	s := m.top()
	switch {
	case k.String() == "ctrl+c":
		return tea.Quit
	case s.kind == homeScreen && m.typing:
		m.typeFilter(k)
		return nil
	}

	switch k.String() {
	case "q":
		return tea.Quit
	case "esc":
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		} else {
			m.filter = ""
		}

		return nil
	}

	switch s.kind {
	case homeScreen:
		return m.homeKey(k)
	case specScreen, buildScreen:
		return m.listKey(s, k)
	}

	switch k.String() {
	case "s":
		if s.kind == runScreen {
			return m.openText(s.spec, s.build)
		}
	case "home", "g":
		s.body.GotoTop()
	case "end", "G":
		s.body.GotoBottom()
	default:
		s.body, _ = s.body.Update(k)
	}

	s.follow = s.body.AtBottom()
	return nil
}

func (m *model) homeKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "tab":
		m.focus = m.nextPane(1)
	case "shift+tab":
		m.focus = m.nextPane(-1)
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "/":
		m.typing, m.focus, m.sel[specsPane] = true, specsPane, 0
	case "enter":
		return m.openTarget(target{paneTargets[m.focus], m.sel[m.focus]})
	}

	return nil
}

var paneTargets = [panes]string{specsPane: toSpec, nowPane: toNow, recentPane: toRecent}

// nextPane is the pane by panes on from the focused one. It skips the now line while nothing runs.
func (m model) nextPane(by int) pane {
	p := m.focus
	for {
		p = (p + pane(by) + panes) % panes
		if m.items(p) > 0 || p == specsPane {
			return p
		}
	}
}

// items counts what a pane lists.
func (m model) items(p pane) int {
	switch p {
	case specsPane:
		return len(m.filtered())
	case nowPane:
		if _, _, ok := running(m.specs); ok {
			return 1
		}

		return 0
	}

	return len(m.recent())
}

func (m *model) move(by int) {
	m.sel[m.focus] = max(0, min(m.items(m.focus)-1, m.sel[m.focus]+by))
}

func (m *model) typeFilter(k tea.KeyPressMsg) {
	switch k.String() {
	case "esc":
		m.typing, m.filter = false, ""
	case "enter":
		m.typing = false
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	default:
		m.filter += k.Text
	}

	m.sel[specsPane] = 0
}

// listKey moves the selection on a spec's screen or a build's, and opens what is selected.
func (m *model) listKey(s *screen, k tea.KeyPressMsg) tea.Cmd {
	n := len(m.builds(s.spec))
	open := target{toBuild, s.sel}
	if s.kind == buildScreen {
		n, open = len(m.runs(s.spec, s.build)), target{toRun, s.sel}
	}

	switch k.String() {
	case "up", "k":
		s.sel, s.reveal = max(0, s.sel-1), true
	case "down", "j":
		s.sel, s.reveal = max(0, min(n-1, s.sel+1)), true
	case "pgdown", "pgup":
		s.body, _ = s.body.Update(k)
	case "enter":
		if s.sel < n {
			return m.openTarget(open)
		}
	case "s":
		build := s.build
		if s.kind == specScreen {
			build = s.sel
		}

		return m.openText(s.spec, build)
	}

	return nil
}

func (m *model) wheel(down bool) {
	s := m.top()
	by := map[bool]int{true: 1, false: -1}[down]
	if s.kind == homeScreen {
		m.move(by)
		return
	}

	if down {
		s.body.ScrollDown(wheelLines)
	} else {
		s.body.ScrollUp(wheelLines)
	}

	s.follow = s.body.AtBottom()
}

// wheelLines is how far one turn of the wheel scrolls.
const wheelLines = 3

// openTarget opens what a row names, as a click or Enter on it does.
func (m *model) openTarget(t target) tea.Cmd {
	s := m.top()
	switch t.to {
	case toSpec:
		specs := m.filtered()
		if t.i >= len(specs) {
			return nil
		}

		m.focus, m.sel[specsPane], m.typing = specsPane, t.i, false
		name := specs[t.i].Name
		m.push(&screen{kind: specScreen, spec: name, sel: max(0, len(m.builds(name))-1), reveal: true})
	case toNow:
		if ref, _, ok := running(m.specs); ok {
			return m.openRun(ref)
		}
	case toRecent:
		if all := m.recent(); t.i < len(all) {
			m.focus, m.sel[recentPane] = recentPane, t.i
			m.push(&screen{kind: buildScreen, spec: all[t.i].spec, build: all[t.i].build})
		}
	case toBuild:
		if t.i < len(m.builds(s.spec)) {
			s.sel = t.i
			m.push(&screen{kind: buildScreen, spec: s.spec, build: t.i})
		}
	case toRun:
		if t.i < len(m.runs(s.spec, s.build)) {
			s.sel = t.i
			return m.openRun(runRef{s.spec, s.build, t.i})
		}
	}

	return nil
}

func (m *model) openRun(ref runRef) tea.Cmd {
	m.push(&screen{kind: runScreen, spec: ref.spec, build: ref.build, run: ref.run, follow: true})
	return m.load()
}

func (m *model) openText(spec string, build int) tea.Cmd {
	builds := m.builds(spec)
	if build >= len(builds) {
		return nil
	}

	m.push(&screen{kind: textScreen, spec: spec, build: build})
	src, b := m.src, builds[build]
	return func() tea.Msg {
		text, err := src.Text(context.Background(), b)
		return texted{spec: spec, build: build, text: text, err: err}
	}
}

// refresh gives the top screen's body its lines, sized to the terminal.
func (m *model) refresh() {
	s := m.top()
	if s.kind == homeScreen {
		return
	}

	if s.kind == textScreen && s.text != "" && s.drawnW != m.w {
		s.drawn, s.drawnW = markdown(s.text, m.w), m.w
	}

	head, body, sel := m.page(s)
	lines := make([]string, len(body))
	for i, r := range body {
		lines[i] = r.text
	}

	s.body.SetWidth(m.w)
	s.body.SetHeight(max(1, m.h-len(head)-1))
	s.body.SetContentLines(lines)
	if s.reveal && sel >= 0 {
		s.body.EnsureVisible(sel, 0, 0)
	}

	if s.follow {
		s.body.GotoBottom()
	}

	s.reveal = false
}

// markdown draws text as Markdown, with its code highlighted, to fit width columns.
func markdown(text string, width int) []string {
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(max(20, width-4)))
	if err == nil {
		if drawn, err := r.Render(text); err == nil {
			return strings.Split(strings.TrimRight(drawn, "\n"), "\n")
		}
	}

	return strings.Split(text, "\n")
}

func (m model) spec(name string) *Spec {
	if i := slices.IndexFunc(m.specs, func(s Spec) bool { return s.Name == name }); i >= 0 {
		return &m.specs[i]
	}

	return nil
}

func (m model) builds(name string) []*Build {
	if s := m.spec(name); s != nil {
		return s.Builds
	}

	return nil
}

func (m model) build(name string, i int) *Build {
	if builds := m.builds(name); i < len(builds) {
		return builds[i]
	}

	return nil
}

func (m model) runs(name string, build int) []*Run {
	if b := m.build(name, build); b != nil {
		return b.runs()
	}

	return nil
}

// filtered are the specs whose names hold the filter's text.
func (m model) filtered() []Spec {
	return slices.DeleteFunc(slices.Clone(m.specs), func(s Spec) bool {
		return !strings.Contains(strings.ToLower(s.Name), strings.ToLower(m.filter))
	})
}

// buildRef is a build, with the spec and index that name it.
type buildRef struct {
	spec  string
	build int
	b     *Build
}

// recent is every build, newest first.
func (m model) recent() []buildRef {
	var all []buildRef
	for _, s := range m.specs {
		for i, b := range s.Builds {
			all = append(all, buildRef{s.Name, i, b})
		}
	}

	slices.SortStableFunc(all, func(a, b buildRef) int { return b.b.Started.Compare(a.b.Started) })
	return all
}

// running finds the run that is running, if one is.
func running(specs []Spec) (runRef, *Run, bool) {
	for _, s := range specs {
		for bi, b := range s.Builds {
			for ri, r := range b.runs() {
				if r.running() {
					return runRef{s.Name, bi, ri}, r, true
				}
			}
		}
	}

	return runRef{}, nil, false
}

func find(specs []Spec, ref runRef) *Run {
	for _, s := range specs {
		if s.Name != ref.spec || ref.build >= len(s.Builds) {
			continue
		}

		if runs := s.Builds[ref.build].runs(); ref.run < len(runs) {
			return runs[ref.run]
		}
	}

	return nil
}
