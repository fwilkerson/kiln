package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

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
	// keysShown lists the top screen's keys over it. Until it is closed, no other key or click reaches the screen.
	keysShown bool

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
	calls  *claude.Recounted
	// anchored keeps the call at the top of a run's view there, as newer calls come in above it. anchor is that
	// call's index in calls.Actions, counted from the oldest. So a read of any age finds the same call.
	anchored bool
	anchor   int
	// text is the spec as approved, or the run's answer, as Markdown. drawn is it drawn for a width of drawnW.
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
		m.top().pin()
	case tea.MouseClickMsg:
		switch {
		case m.keysShown:
			m.keysShown = false
		case msg.Button == tea.MouseLeft:
			t, _ := parseTarget(m.layout().Hit(msg.X, msg.Y).ID())
			cmd = m.openTarget(t)
		}
	case tea.MouseWheelMsg:
		if !m.keysShown {
			m.wheel(msg.X, msg.Y, msg.Button == tea.MouseWheelDown)
			m.top().pin()
		}
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
		if msg.open.Answer != s.text {
			s.text, s.drawn = msg.open.Answer, nil
		}
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
	switch {
	case k.String() == "ctrl+c":
		return tea.Quit
	case m.top().kind == homeScreen && m.typing:
		m.typeFilter(k)
		return nil
	case !m.keysShown:
		return m.do(k)
	case key.Matches(k, quit):
		return tea.Quit
	case key.Matches(k, keysList, back):
		m.keysShown = false
	}

	return nil
}

var paneTargets = [panes]string{specsPane: toSpec, nowPane: toNow, recentPane: toRecent}

// paneAt is the home screen's pane drawn at column x, row y, if one is.
func (m model) paneAt(x, y int) (pane, bool) {
	t, ok := parseTarget(m.layout().Hit(x, y).ID())
	switch {
	case !ok:
		return 0, false
	case t.to == toPane:
		return pane(t.i), true
	}

	p := slices.Index(paneTargets[:], t.to)
	return pane(p), p >= 0
}

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

// rows counts the rows of a spec's screen or a build's that a key selects.
func (m model) rows(s *screen) int {
	if s.kind == specScreen {
		return len(m.builds(s.spec))
	}

	return len(m.runs(s.spec, s.build))
}

// row is the selection's row, from the top. A spec's screen lists its builds newest first, and a build's its runs
// in order.
func (m model) row(s *screen) int {
	if s.kind == specScreen {
		return m.rows(s) - 1 - s.sel
	}

	return s.sel
}

// selectRow selects the row at i from the top, or the nearest row there is.
func (m model) selectRow(s *screen, i int) {
	i = max(0, min(m.rows(s)-1, i))
	if s.kind == specScreen {
		i = m.rows(s) - 1 - i
	}

	s.sel, s.reveal = max(0, i), true
}

// wheel scrolls what is under the pointer, at column x and row y. On home, that is the pane it is over, which takes
// the keys too.
func (m *model) wheel(x, y int, down bool) {
	s := m.top()
	by := map[bool]int{true: 1, false: -1}[down]
	if s.kind == homeScreen {
		if p, ok := m.paneAt(x, y); ok {
			m.focus = p
			m.move(by)
		}

		return
	}

	if down {
		s.body.ScrollDown(wheelLines)
	} else {
		s.body.ScrollUp(wheelLines)
	}
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
	m.push(&screen{kind: runScreen, spec: ref.spec, build: ref.build, run: ref.run})
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

	if s.text != "" && (s.drawn == nil || s.drawnW != m.w) {
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
	// A wider terminal may need less sideways scroll than the view has.
	s.body.SetXOffset(s.body.XOffset())
	if s.reveal && sel >= 0 {
		s.body.EnsureVisible(sel, 0, 0)
	}

	if s.anchored {
		s.body.SetYOffset(s.callsFrom() + len(s.calls.Actions) - 1 - s.anchor)
	}

	s.reveal = false
}

// callsFrom is the line of a run's body its newest call is on, below its answer.
func (s *screen) callsFrom() int {
	if len(s.drawn) == 0 {
		return 0
	}

	return len(s.drawn) + 1
}

// pin anchors the call at the top of a run's view. At the view's top, or above the calls, the view keeps its offset
// instead. So at the top it stays at the top, where the newest call comes in.
func (s *screen) pin() {
	if s.kind != runScreen || s.calls == nil {
		return
	}

	row := s.body.YOffset() - s.callsFrom()
	s.anchored = s.body.YOffset() > 0 && row >= 0 && row < len(s.calls.Actions)
	s.anchor = len(s.calls.Actions) - 1 - row
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
