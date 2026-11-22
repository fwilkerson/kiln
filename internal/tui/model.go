package tui

import (
	"context"
	"errors"
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
	// live maps each run in progress to a stretch of the run's stream, for the run's now line.
	live map[runRef]*claude.Recounted
	w, h int
	// theme is the dark one until the terminal says its background is light.
	theme theme
	// stack holds the screens the developer opened, home first. Esc takes the top one off.
	stack []*screen
	// open names the spec to select on home once the source is first read.
	open string
	// keysShown lists the top screen's keys over it. Until it is closed, no other key or click reaches the screen.
	keysShown bool
	// keysTop is the first key the key list shows, where the list scrolls.
	keysTop int

	// The home screen's. Each side panel's first row stands for all it lists, so its sel is 0 there. On a spec or a
	// monitor, it is the index in sideSpecs, plus 1. side is the side panel whose selection narrows the builds panel
	// and the trend.
	focus  pane
	side   pane
	filter string
	typing bool
	sel    [panes]int
}

// pane is a part of the home screen that tab moves the keys to.
type pane int

const (
	specsPane pane = iota
	monitorsPane
	nowPane
	recentPane
	panes
)

type kind int

const (
	homeScreen kind = iota
	buildScreen
	runScreen
	textScreen
)

// screen is one screen the developer opened. Its spec, build and run say what it shows, as far as its kind needs.
type screen struct {
	kind kind
	runRef
	// sel is the selected run, on a build's screen.
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
	return model{src: src, every: every, open: name, w: 80, h: 24, theme: newTheme(true),
		stack: []*screen{{kind: homeScreen}}}
}

// tick asks the view to read its source again.
type tick struct{}

// loaded is what one read of the source found.
type loaded struct {
	at    time.Time
	specs []Spec
	err   error
	live  map[runRef]*claude.Recounted
	// opened is the run the top screen showed when the read began, and open its stretch of the stream.
	opened runRef
	open   *claude.Recounted
}

// texted is a build's spec as approved, read.
type texted struct {
	ref  buildRef
	text string
	err  error
}

// specRef names a spec, or a monitor. A monitor may share a spec's name.
type specRef struct {
	spec    string
	monitor bool
}

// ref is what names s among the specs and monitors.
func (s Spec) ref() specRef { return specRef{s.Name, s.Monitor != nil} }

// names reports whether r names s.
func (r specRef) names(s Spec) bool { return s.ref() == r }

// buildRef names a build by its spec, and its index among the spec's builds.
type buildRef struct {
	specRef
	build int
}

// runRef names a run by its build, and its index among the build's runs.
type runRef struct {
	buildRef
	run int
}

func (m model) Init() tea.Cmd { return tea.Batch(m.load(), m.tick(), tea.RequestBackgroundColor) }

func (m model) tick() tea.Cmd {
	if m.every == 0 {
		return nil
	}

	return tea.Tick(m.every, func(time.Time) tea.Msg { return tick{} })
}

// load reads the source, the stream of each run in progress, and the stream of the top screen's run.
func (m model) load() tea.Cmd {
	src, opened, open := m.src, runRef{}, false
	if s := m.top(); s.kind == runScreen {
		opened, open = s.runRef, m.rereads(s)
	}

	return func() tea.Msg {
		specs, err := src.Specs(context.Background())
		monitors, monitorsErr := src.Monitors(context.Background())
		specs, err = append(specs, monitors...), errors.Join(err, monitorsErr)
		live := map[runRef]*claude.Recounted{}
		for _, ref := range atWork(specs) {
			live[ref] = recount(src, find(specs, ref))
		}

		msg := loaded{at: time.Now(), specs: specs, err: err, live: live, opened: opened}
		if open {
			msg.open = recount(src, find(specs, opened))
		}

		return msg
	}
}

// rereads reports whether a load reads the stream of the run s shows. An ended run's stream no longer grows, so it
// is read once.
func (m model) rereads(s *screen) bool {
	r := find(m.specs, s.runRef)
	return s.calls == nil || r == nil || r.running()
}

// recount reads the run's stretch of its session's stream. It is nil for no run, or for a read that failed.
func recount(src Source, r *Run) *claude.Recounted {
	if r == nil {
		return nil
	}

	counted, err := src.Recount(r)
	if err != nil {
		return nil
	}

	return &counted
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		m.theme = newTheme(msg.IsDark())
		for _, s := range m.stack {
			s.drawn = nil
		}
	case tick:
		cmd = tea.Batch(m.load(), m.tick())
	case loaded:
		m.loaded(msg)
	case texted:
		m.texted(msg)
	case tea.KeyPressMsg:
		cmd = m.key(msg)
		m.top().pin()
	case tea.MouseClickMsg:
		cmd = m.click(msg)
	case tea.MouseWheelMsg:
		m.wheel(msg)
	}

	m.refresh()
	return m, cmd
}

func (m *model) loaded(msg loaded) {
	m.now, m.err, m.live = msg.at, msg.err, msg.live
	if msg.err == nil {
		m.keep(msg.specs)
	}

	if s := m.top(); s.kind == runScreen && s.runRef == msg.opened {
		s.recounted(msg.open)
	}

	// A name kiln holds no spec of becomes the filter, which then says so.
	if m.open != "" && !m.pick(func(s Spec) bool { return s.Name == m.open }) {
		m.filter = m.open
	}

	m.open = ""
}

// keep takes the specs as read anew. The spec and the build selected on home stay selected, wherever a new spec or
// build moves them to.
func (m *model) keep(specs []Spec) {
	spec, picked := m.picked()
	build, held := m.homeBuild(recentPane)
	m.specs = specs
	if picked {
		m.pick(spec.ref().names)
	}

	if i := slices.IndexFunc(m.recent(), func(l listed) bool { return l.buildRef == build }); held && i >= 0 {
		m.sel[recentPane] = i
	}
}

// picked is the spec or monitor selected in the side panel that narrows the builds, if one is. A side panel's first
// row selects none, for the builds of all it lists.
func (m model) picked() (Spec, bool) {
	specs := m.sideSpecs(m.side)
	if i := m.sel[m.side] - 1; i >= 0 && i < len(specs) {
		return specs[i], true
	}

	return Spec{}, false
}

// pick selects the first spec, or else the first monitor, for which is reports true. When none does, it selects the
// row for all.
func (m *model) pick(is func(Spec) bool) bool {
	for _, p := range []pane{specsPane, monitorsPane} {
		if i := slices.IndexFunc(m.sideSpecs(p), is); i >= 0 {
			m.selectSide(p, i+1)
			return true
		}
	}

	m.selectSide(m.side, 0)
	return false
}

// selectSide selects row i of side panel p, which then narrows the builds panel, at its newest build.
func (m *model) selectSide(p pane, i int) {
	if p != m.side || i != m.sel[p] {
		m.side, m.sel[p], m.sel[recentPane] = p, i, 0
	}
}

// focusOn gives pane p the keys. A side panel's selection then narrows the builds panel.
func (m *model) focusOn(p pane) {
	m.focus = p
	if p.side() {
		m.selectSide(p, m.sel[p])
	}
}

// side says whether p is a side panel, the specs' or the monitors'.
func (p pane) side() bool { return p == specsPane || p == monitorsPane }

// noun names n builds of what side panel p lists, without the count: a spec's builds, or a monitor's fixes.
func (p pane) noun(n int) string {
	switch {
	case p == monitorsPane && n == 1:
		return "fix"
	case p == monitorsPane:
		return "fixes"
	case n == 1:
		return "build"
	}

	return "builds"
}

// lists says whether side panel p lists s: the monitors' panel lists monitors, and the specs' panel the rest.
func (p pane) lists(s Spec) bool { return (s.Monitor != nil) == (p == monitorsPane) }

// homeBuild is the build home's pane p points at, if any. That is the selected spec's newest, the running one, or
// the one selected among the builds.
func (m model) homeBuild(p pane) (buildRef, bool) {
	switch p {
	case specsPane, monitorsPane:
		s, ok := m.picked()
		return buildRef{s.ref(), len(s.Builds) - 1}, ok && len(s.Builds) > 0
	case nowPane:
		ref, ok := m.nowPicked()
		return ref.buildRef, ok
	}

	all := m.recent()
	if i := m.sel[recentPane]; i < len(all) {
		return all[i].buildRef, true
	}

	return buildRef{}, false
}

// recounted gives a run's screen its stream as read, if one was. An answer that changed is drawn again.
func (s *screen) recounted(c *claude.Recounted) {
	if c == nil {
		return
	}

	s.calls = c
	if c.Answer != s.text {
		s.text, s.drawn = c.Answer, nil
	}
}

// texted gives the spec text read to the top screen, if that is the text screen that asked for it.
func (m *model) texted(msg texted) {
	s := m.top()
	if s.kind != textScreen || s.buildRef != msg.ref {
		return
	}

	s.text = msg.text
	if msg.err != nil {
		s.text = "kiln could not read the spec: " + msg.err.Error()
	}
}

// click opens the row a left click hits. On a spec, it selects it, which shows its builds. While the key list
// shows, a click only closes it.
func (m *model) click(msg tea.MouseClickMsg) tea.Cmd {
	if m.keysShown || msg.Button != tea.MouseLeft {
		m.keysShown = false
		return nil
	}

	t, _ := m.hit(msg.Mouse())
	p := pane(slices.Index(paneTargets[:], t.to))
	if !p.side() {
		return m.openTarget(t)
	}

	m.focus, m.typing = p, false
	m.selectSide(p, t.i)
	return nil
}

// hit is the target of the row drawn under the pointer, if one is.
func (m model) hit(at tea.Mouse) (target, bool) { return parseTarget(m.layout().Hit(at.X, at.Y).ID()) }

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
	case key.Matches(k, keyDown):
		m.scrollKeys(1)
	case key.Matches(k, keyUp):
		m.scrollKeys(-1)
	}

	return nil
}

var paneTargets = [panes]string{specsPane: toSpec, monitorsPane: toMonitor, nowPane: toNow, recentPane: toRecent}

// paneAt is the home screen's pane drawn under the pointer, if one is.
func (m model) paneAt(at tea.Mouse) (pane, bool) {
	t, ok := m.hit(at)
	switch {
	case !ok:
		return 0, false
	case t.to == toPane:
		return pane(t.i), true
	}

	p := slices.Index(paneTargets[:], t.to)
	return pane(p), p >= 0
}

// nextPane is the pane by panes on from the focused one. It skips the now line while nothing runs, and the monitors
// panel while there is no monitor.
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
		return len(m.sideSpecs(specsPane)) + 1
	case monitorsPane:
		if !slices.ContainsFunc(m.specs, monitorsPane.lists) {
			return 0
		}

		return len(m.sideSpecs(monitorsPane)) + 1
	case nowPane:
		return len(atWork(m.specs))
	}

	return len(m.recent())
}

func (m *model) move(by int) {
	to := max(0, min(m.items(m.focus)-1, m.sel[m.focus]+by))
	if m.focus.side() {
		m.selectSide(m.focus, to)
		return
	}

	m.sel[m.focus] = to
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

	// The first spec the filter leaves, or else its first monitor, so Enter then opens it.
	p := specsPane
	if len(m.sideSpecs(specsPane)) == 0 && len(m.sideSpecs(monitorsPane)) > 0 {
		p = monitorsPane
	}

	m.focus = p
	m.selectSide(p, min(1, len(m.sideSpecs(p))))
	m.sel[recentPane] = 0
}

// selectRun selects run i of a build's screen, or the nearest run there is.
func (m model) selectRun(s *screen, i int) {
	s.sel, s.reveal = max(0, min(len(m.runs(s.buildRef))-1, i)), true
}

// wheel scrolls what is under the pointer. On home, that is the pane it is over, which takes the keys too. While the
// key list shows, the wheel does nothing.
func (m *model) wheel(msg tea.MouseWheelMsg) {
	if m.keysShown {
		return
	}

	s := m.top()
	switch {
	case s.kind == homeScreen:
		m.wheelPane(msg)
	case msg.Button == tea.MouseWheelDown:
		s.body.ScrollDown(wheelLines)
	default:
		s.body.ScrollUp(wheelLines)
	}

	s.pin()
}

// wheelLines is how far one turn of the wheel scrolls.
const wheelLines = 3

// wheelPane moves the selection of the home screen's pane under the pointer, and gives that pane the keys.
func (m *model) wheelPane(msg tea.MouseWheelMsg) {
	p, ok := m.paneAt(msg.Mouse())
	if !ok {
		return
	}

	m.focusOn(p)
	m.move(map[bool]int{true: 1, false: -1}[msg.Button == tea.MouseWheelDown])
}

// openTarget opens what a row names, as a click or Enter on it does.
func (m *model) openTarget(t target) tea.Cmd {
	switch t.to {
	case toSpec, toMonitor:
		m.openPicked()
	case toNow:
		// A lane whose run ended since leaves the selection past the last run.
		if refs := atWork(m.specs); len(refs) > 0 {
			return m.openRun(refs[min(t.i, len(refs)-1)])
		}
	case toRecent:
		m.openRecent(t)
	case toRun:
		return m.openRunRow(t)
	}

	return nil
}

// openPicked opens the newest build of the spec or monitor selected on home. On a row for all, it gives the builds
// panel the keys instead.
func (m *model) openPicked() {
	if _, ok := m.picked(); !ok && m.items(recentPane) > 0 {
		m.focus = recentPane
	}

	if ref, ok := m.homeBuild(specsPane); ok {
		m.push(&screen{kind: buildScreen, buildRef: ref})
	}
}

// openRecent opens the build at a row of the home screen's builds.
func (m *model) openRecent(t target) {
	if all := m.recent(); t.i < len(all) {
		m.focus, m.sel[recentPane] = recentPane, t.i
		m.push(&screen{kind: buildScreen, buildRef: all[t.i].buildRef})
	}
}

// openRunRow opens the run at a row of a build's screen.
func (m *model) openRunRow(t target) tea.Cmd {
	s := m.top()
	if t.i >= len(m.runs(s.buildRef)) {
		return nil
	}

	s.sel = t.i
	return m.openRun(runRef{s.buildRef, t.i})
}

func (m *model) openRun(ref runRef) tea.Cmd {
	m.push(&screen{kind: runScreen, runRef: ref})
	return m.load()
}

func (m *model) openText(ref buildRef) tea.Cmd {
	b := m.build(ref)
	if b == nil {
		return nil
	}

	m.push(&screen{kind: textScreen, buildRef: ref})
	src := m.src
	return func() tea.Msg {
		text, err := src.Text(context.Background(), b)
		return texted{ref: ref, text: text, err: err}
	}
}

// refresh gives the top screen's body its lines, sized to the terminal.
func (m *model) refresh() {
	s := m.top()
	if s.kind == homeScreen {
		return
	}

	m.drawText(s)
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

// drawText draws the screen's text for the terminal's width, unless it is drawn for that width already.
func (m model) drawText(s *screen) {
	switch {
	case s.text == "":
	case s.drawn == nil, s.drawnW != m.w:
		s.drawn, s.drawnW = markdown(s.text, m.w, m.theme.markdown), m.w
	}
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

func (m model) spec(ref specRef) *Spec {
	if i := slices.IndexFunc(m.specs, ref.names); i >= 0 {
		return &m.specs[i]
	}

	return nil
}

func (m model) builds(ref specRef) []*Build {
	if s := m.spec(ref); s != nil {
		return s.Builds
	}

	return nil
}

func (m model) build(ref buildRef) *Build {
	if builds := m.builds(ref.specRef); ref.build < len(builds) {
		return builds[ref.build]
	}

	return nil
}

func (m model) runs(ref buildRef) []*Run {
	if b := m.build(ref); b != nil {
		return b.runs()
	}

	return nil
}

// sideSpecs are the specs side panel p lists whose names hold the filter's text.
func (m model) sideSpecs(p pane) []Spec {
	return slices.DeleteFunc(slices.Clone(m.specs), func(s Spec) bool {
		return !p.lists(s) || !strings.Contains(strings.ToLower(s.Name), strings.ToLower(m.filter))
	})
}

// shown are the specs whose builds home's builds panel and trend hold. That is the one selected, or else every one
// the side panel lists, whatever the filter.
func (m model) shown() []Spec {
	if s, ok := m.picked(); ok {
		return []Spec{s}
	}

	return slices.DeleteFunc(slices.Clone(m.specs), func(s Spec) bool { return !m.side.lists(s) })
}

// listed is a build, with the ref that names it.
type listed struct {
	buildRef
	b *Build
}

// recent is every build of the specs home shows, newest first.
func (m model) recent() []listed {
	var all []listed
	for _, s := range m.shown() {
		for i, b := range s.Builds {
			all = append(all, listed{buildRef{s.ref(), i}, b})
		}
	}

	slices.SortStableFunc(all, func(a, b listed) int { return b.b.Started.Compare(a.b.Started) })
	return all
}

// atWork finds the run at work in each lane, if one is: a spec's, then a fix's. Each is the first running run among
// its lane's specs, or among the monitors.
func atWork(specs []Spec) []runRef {
	var refs []runRef
	for _, fixes := range []bool{false, true} {
		if ref, ok := running(specs, fixes); ok {
			refs = append(refs, ref)
		}
	}

	return refs
}

// nowPicked is the run selected in the now pane, if a run is at work.
func (m model) nowPicked() (runRef, bool) {
	refs := atWork(m.specs)
	if len(refs) == 0 {
		return runRef{}, false
	}

	return refs[min(m.sel[nowPane], len(refs)-1)], true
}

// running finds the first run that is running among the monitors when fixes is true, or else among the specs.
func running(specs []Spec, fixes bool) (runRef, bool) {
	for _, s := range specs {
		if (s.Monitor != nil) != fixes {
			continue
		}

		for bi, b := range s.Builds {
			runs := b.runs()
			if ri := slices.IndexFunc(runs, (*Run).running); ri >= 0 {
				return runRef{buildRef{s.ref(), bi}, ri}, true
			}
		}
	}

	return runRef{}, false
}

func find(specs []Spec, ref runRef) *Run {
	for _, s := range specs {
		if s.ref() != ref.specRef || ref.build >= len(s.Builds) {
			continue
		}

		if runs := s.Builds[ref.build].runs(); ref.run < len(runs) {
			return runs[ref.run]
		}
	}

	return nil
}
