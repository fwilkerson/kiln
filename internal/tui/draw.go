package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fwilkerson/kiln/internal/claude"
)

func (m model) View() tea.View {
	v := tea.NewView(m.layout().Render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "kiln"
	return v
}

// target is what a click on a row opens, or the home screen's pane a layer draws. Only id writes a layer's ID, and
// only parseTarget reads one.
type target struct {
	to string
	i  int
}

const (
	toSpec    = "spec"
	toMonitor = "monitor"
	toNow     = "now"
	toRecent  = "recent"
	toRun     = "run"
	// toPane names a home screen's pane, by its number. A click there opens nothing, and the wheel scrolls it.
	toPane = "pane"
)

func (t target) id() string {
	if t.to == "" {
		return ""
	}

	return t.to + ":" + strconv.Itoa(t.i)
}

// parseTarget reads a layer's ID back as the target it names. It answers false for a layer that names none.
func parseTarget(id string) (target, bool) {
	to, n, _ := strings.Cut(id, ":")
	i, err := strconv.Atoi(n)
	if err != nil || !slices.Contains([]string{toSpec, toMonitor, toNow, toRecent, toRun, toPane}, to) {
		return target{}, false
	}

	return target{to, i}, true
}

// row is one line on screen. A row with a target is a layer of its own, which a click finds.
type row struct {
	text string
	to   target
}

// dotted joins what a line says, with a dim dot between each two, so each reads apart.
func (t theme) dotted(parts ...string) string { return strings.Join(parts, t.dim.Render(" · ")) }

// layout places every row of the top screen in a terminal of m.w by m.h cells. The view draws it, and a click asks it
// which row it hit. A terminal too small for the top screen gets one line that says so.
func (m model) layout() *lipgloss.Compositor {
	s := m.top()
	if w := m.minW(s.kind); m.w < w || m.h < minH {
		return lipgloss.NewCompositor(lipgloss.NewLayer(fit(fmt.Sprintf("needs %d×%d cells", w, minH), m.w)))
	}

	layers := []*lipgloss.Layer{lipgloss.NewLayer(strings.Repeat(strings.Repeat(" ", m.w)+"\n", m.h-1) +
		strings.Repeat(" ", m.w))}
	if m.keysShown {
		layers = append(layers, m.keyList(s.kind))
	}

	if s.kind == homeScreen {
		return lipgloss.NewCompositor(append(layers, m.home()...)...)
	}

	head, body, _ := m.page(s)
	off := s.body.YOffset()
	shown := slices.Clone(body[min(off, len(body)):min(off+s.body.Height(), len(body))])
	if x := s.body.XOffset(); x > 0 {
		for i, r := range shown {
			shown[i].text = ansi.Cut(r.text, x, x+m.w)
		}
	}

	layers = append(layers, m.place(append(head, shown...))...)
	return lipgloss.NewCompositor(append(layers, m.foot(s.kind))...)
}

// minW is the narrowest terminal a screen of kind k draws in. That is its key list as one column, and homeW on home.
func (m model) minW(k kind) int {
	if k == homeScreen {
		return max(homeW, m.theme.keysW(k))
	}

	return m.theme.keysW(k)
}

// homeW is the narrowest home: its side panel and its right column take 20 columns each.
const homeW = 40

// minH is the shortest terminal the view draws in: a run's head, a row of its body, and the footer.
const minH = 5

// place makes a layer of each row, one below the other, each cut or padded to the terminal's width.
func (m model) place(rows []row) []*lipgloss.Layer {
	layers := make([]*lipgloss.Layer, len(rows))
	for i, r := range rows {
		layers[i] = lipgloss.NewLayer(fit(r.text, m.w)).ID(r.to.id()).Y(i).Z(1)
	}

	return layers
}

// fit cuts s to w cells, or pads it to them.
func fit(s string, w int) string {
	s = ansi.Truncate(s, max(0, w), "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// cols lays cells out in columns of the widths given. A negative width aligns its cell to the right.
func cols(widths []int, cells ...string) string {
	var b strings.Builder
	for i, c := range cells {
		if i > 0 {
			b.WriteString(" ")
		}

		if i >= len(widths) {
			b.WriteString(c)
			continue
		}

		w := widths[i]
		width := max(w, -w)
		c = cut(c, width)
		pad := strings.Repeat(" ", max(0, width-ansi.StringWidth(c)))
		if w < 0 {
			b.WriteString(pad + c)
		} else {
			b.WriteString(c + pad)
		}
	}

	return b.String()
}

// cut cuts s to w cells. It keeps the end of an absolute path, which names the file, and the start of anything else.
func cut(s string, w int) string {
	if over := ansi.StringWidth(s) - w; over > 0 && strings.HasPrefix(s, "/") {
		return ansi.TruncateLeft(s, over+1, "…")
	}

	return ansi.Truncate(s, w, "…")
}

// pick draws a selected row in the picked style, in no other color.
func (t theme) pick(text string, selected bool) string {
	if selected {
		return t.picked.Render(ansi.Strip(text))
	}

	return text
}

func (m model) foot(k kind) *lipgloss.Layer {
	text := m.theme.footer(k, m.w-1)
	if m.err != nil {
		text = m.theme.failed.Render("✗ kiln could not read its ledger: " + m.err.Error())
	}

	return lipgloss.NewLayer(fit(" "+text, m.w)).Y(m.h - 1).Z(1)
}

// panel is a box on the home screen, at column x and row y, the size of its area. Its title is in its top border. A
// wheel turned over it scrolls the pane to names, if any. A lit panel is the one the keys go to.
type panel struct {
	to    target
	title string
	lit   bool
	x, y  int
	area
}

// area is a size in cells: w columns by h rows.
type area struct{ w, h int }

// budget is the cells left to give out along one side of the screen. Each panel takes its size from one, so the
// panels never add up to more than the screen.
type budget int

// take gives want cells, or as many as are left, and leaves the rest.
func (b *budget) take(want int) int {
	n := max(0, min(want, int(*b)))
	*b -= budget(n)
	return n
}

// inside is the area within the panel's border.
func (p panel) inside() area { return area{p.w - 2, p.h - 2} }

// box draws the panel, with its rows inside it from the top. A panel too short for its rows says in its title how
// many it cut, as +2. So no row goes unseen without a mark.
func (p panel) box(t theme, rows []row) *lipgloss.Layer {
	if p.h < 2 {
		return lipgloss.NewLayer("").X(p.x).Y(p.y)
	}

	edge, name := t.frame, t.bold
	if p.lit {
		edge, name = t.you, t.you.Bold(true)
	}

	inner := max(0, p.w-2)
	cut := ""
	if n := len(rows) - max(0, p.h-2); n > 0 {
		cut = fmt.Sprintf(" +%d", n)
	}

	title := ansi.Truncate(p.title, max(0, inner-2-len(cut)), "…") + cut
	lines := []string{edge.Render("╭ ") + name.Render(title) + edge.Render(" "+
		strings.Repeat("─", max(0, inner-ansi.StringWidth(title)-2))+"╮")}
	for range max(0, p.h-2) {
		lines = append(lines, edge.Render("│")+strings.Repeat(" ", inner)+edge.Render("│"))
	}

	lines = append(lines, edge.Render("╰"+strings.Repeat("─", inner)+"╯"))
	layer := lipgloss.NewLayer(strings.Join(lines, "\n")).ID(p.to.id()).Z(1)
	for i, r := range rows[:min(len(rows), max(0, p.h-2))] {
		layer.AddLayers(lipgloss.NewLayer(fit(r.text, inner)).ID(r.to.id()).X(1).Y(1 + i).Z(2))
	}

	return layer.X(p.x).Y(p.y)
}

func (m model) home() []*lipgloss.Layer {
	across, h := budget(m.w), m.h-1
	sideW := across.take(m.sideWidth())
	rightW := across.take(m.w)
	now := m.nowRows()
	tiles := m.trendRows(rightW - 2)
	recentBuilds, previous := trend(m.shown())
	gauges := m.gaugeRows(rightW - 2)
	// The trend and the gauges each leave the builds 5 rows, but keep 3 rows while any are left.
	down := budget(h)
	nowH := down.take(len(now) + 2)
	trendH := down.take(min(len(tiles)+2, max(3, int(down)-5)))
	gaugesH := 0
	if len(gauges) > 0 {
		gaugesH = down.take(min(len(gauges)+2, max(3, int(down)-5)))
	}

	recentH := down.take(h)
	monitorsH := 0
	if n := m.items(monitorsPane); n > 0 {
		monitorsH = min(n+2, h/2)
	}

	specsBox := panel{to: target{toPane, int(specsPane)}, title: "specs", lit: m.focus == specsPane,
		area: area{sideW, h - monitorsH}}
	monitorsBox := panel{to: target{toPane, int(monitorsPane)}, title: "monitors", lit: m.focus == monitorsPane,
		y: h - monitorsH, area: area{sideW, monitorsH}}
	nowBox := panel{to: target{toPane, int(nowPane)}, title: "now", lit: m.focus == nowPane, x: sideW,
		area: area{rightW, nowH}}
	trendBox := panel{title: m.trendTitle(recentBuilds, previous), x: sideW, y: nowH, area: area{rightW, trendH}}
	gaugesBox := panel{title: "gauges", x: sideW, y: nowH + trendH, area: area{rightW, gaugesH}}
	recentBox := panel{to: target{toPane, int(recentPane)}, title: m.recentTitle(), lit: m.focus == recentPane,
		x: sideW, y: nowH + trendH + gaugesH, area: area{rightW, recentH}}
	layers := []*lipgloss.Layer{
		specsBox.box(m.theme, m.sideRows(specsPane, specsBox.inside())),
		nowBox.box(m.theme, now),
		trendBox.box(m.theme, tiles),
		recentBox.box(m.theme, m.recentRows(recentBox.inside())),
		m.foot(homeScreen),
	}

	if monitorsH > 0 {
		layers = append(layers, monitorsBox.box(m.theme, m.sideRows(monitorsPane, monitorsBox.inside())))
	}

	if gaugesH > 0 {
		layers = append(layers, gaugesBox.box(m.theme, gauges))
	}

	return layers
}

// recentTitle names what the builds panel lists: the builds of every spec, of every monitor, or of the selected one.
func (m model) recentTitle() string {
	s, ok := m.picked()
	switch {
	case !ok:
		return "recent " + m.side.noun(2)
	case s.Monitor != nil:
		return monitorTitle(s)
	case len(s.Builds) == 0:
		return "builds of " + s.Name
	}

	return fmt.Sprintf("builds of %s · approved %s", s.Name, times(len(s.Builds)))
}

// sideWidth fits the side panels to their longest row, but takes at most a third of the terminal.
func (m model) sideWidth() int {
	need := 0
	for _, s := range m.specs {
		need = max(need, ansi.StringWidth(s.Name)+len(s.State.String())+3)
	}

	return max(20, min(need+2, 40, m.w/3))
}

// sideRows draws side panel p in an area. Below a row for all, it lists each of its specs whose name holds the
// filter. The specs panel holds the filter above them.
func (m model) sideRows(p pane, in area) []row {
	var rows []row
	all, none := "≡ all monitors", "no monitor"
	if p == specsPane {
		rows = []row{{text: m.filterRow()}, {}}
		all, none = "≡ all specs", "no spec"
	}

	specs := m.sideSpecs(p)
	room := max(1, in.h-len(rows))
	sel := m.sel[p]
	for i := max(0, sel-room+1); i <= len(specs) && len(rows) < in.h; i++ {
		text := all
		if i > 0 {
			s := specs[i-1]
			text = m.theme.marked(s.State) + " " + fit(s.Name, max(1, in.w-3-len(s.State.String()))) + " " +
				m.theme.stated(s.State)
		}

		rows = append(rows, row{m.theme.pick(text, m.focus == p && i == sel), target{paneTargets[p], i}})
	}

	if len(specs) == 0 && len(rows) < in.h {
		rows = append(rows, row{text: m.theme.dim.Render(none)})
	}

	return rows
}

// filterRow is the filter as the specs panel shows it: its text, with a cursor while it takes keys.
func (m model) filterRow() string {
	switch {
	case m.typing:
		return "/ " + m.filter + "▏"
	case m.filter != "":
		return "/ " + m.filter
	}

	return m.theme.dim.Render("/ filter")
}

// stated is a state's word. A spec that needs nothing more is dim, so the ones in play stand out.
func (t theme) stated(s State) string {
	if s == Accepted || s == Rejected {
		return t.dim.Render(s.String())
	}

	return s.String()
}

// marked is a state's glyph, in its color, a spec's or a monitor's.
func (t theme) marked(s State) string {
	style, glyph := t.look(s)
	return style.Render(glyph)
}

// look is a state's color and glyph. A session runs a running spec, and kiln paused or halted one. What waits on the
// developer is theirs. A state with nothing to mark is plain, beside ◌.
func (t theme) look(s State) (lipgloss.Style, string) {
	switch s {
	case Running, Limited:
		return t.agent, "▶"
	case Paused:
		return t.kiln, "‖"
	case Halted:
		return t.kiln, "!"
	case Stopped, Failed, CouldNotRun, Refused:
		return t.failed, "✗"
	case UAT, Waits, NeedsYou:
		return t.you, "◆"
	case Accepted, Landed, Healthy:
		return t.passed, "✓"
	case Rejected:
		return t.dim, "✗"
	case NotEnded, Queued, CutShort, NeverRun, Unchanged:
	}

	return lipgloss.NewStyle(), "◌"
}

// nowRows are the now pane's rows: two for the run at work in each lane, the spec's above the fix's. With no spec's
// run at work, a paused spec takes the spec's place, in one row.
func (m model) nowRows() []row {
	var rows []row
	if _, ok := running(m.specs, false); !ok {
		if i := slices.IndexFunc(m.specs, func(s Spec) bool { return s.State == Paused }); i >= 0 {
			rows = append(rows, row{text: m.theme.kiln.Render("‖ " + m.specs[i].Name + " · paused for a usage limit")})
		}
	}

	for i, ref := range atWork(m.specs) {
		rows = append(rows, m.runRows(ref, i)...)
	}

	if len(rows) == 0 {
		return []row{{text: m.theme.dim.Render("nothing is running")}}
	}

	return rows
}

// runRows are the two rows of the run ref, at work, which the now pane lists i-th among its runs.
func (m model) runRows(ref runRef, i int) []row {
	t, r := m.theme, find(m.specs, ref)
	first := t.dotted(t.agent.Render("▶")+" "+t.bold.Render(ref.spec), t.agent.Render(r.Role)+" role", r.why(),
		"running "+span(m.now.Sub(r.Start)))
	second := t.dim.Render("reading its stream")
	if live := m.live[ref]; live != nil {
		second = strings.Join(m.liveFacts(live), t.dim.Render("  ·  "))
	}

	to := target{toNow, i}
	picked := m.focus == nowPane && i == min(m.sel[nowPane], len(atWork(m.specs))-1)
	return []row{{t.pick(first, picked), to}, {second, to}}
}

// liveFacts are what the stream of a run in progress says. They are the time since the stream was last written, the
// open call, the run's refusals, and the run's context.
func (m model) liveFacts(c *claude.Recounted) []string {
	idle := m.now.Sub(c.Written)
	open := slices.IndexFunc(c.Actions, func(a claude.Action) bool { return a.Open })
	said := "idle " + clock(idle)
	switch {
	case open >= 0:
		said += ", in a call: " + c.Actions[open].Input
	case idle > idleWarned:
		said = m.theme.you.Render("▶ " + said + ", in no call")
	default:
		said += ", in no call"
	}

	facts := []string{said, fmt.Sprintf("%d refused", tally(c.Actions, isRefused))}
	if ctx := c.Totals.Context; ctx != nil {
		facts = append(facts, "context "+tokens(ctx.Last))
	}

	return facts
}

// idleWarned is how long a session may write nothing, with no call open, before the view marks it.
const idleWarned = time.Minute

// monitorTitle names a monitor's fixes, and says when it last ran and runs next.
func monitorTitle(s Spec) string {
	mon, when := s.Monitor, "never ran"
	if !mon.Last.IsZero() {
		when = "ran " + mon.Last.Local().Format("Jan 02 15:04")
	}

	next := "due"
	if !mon.Due {
		next = "next " + mon.Next.Local().Format("Jan 02 15:04")
	}

	title := "fixes of " + s.Name
	if mon.Result != "" {
		title += " · " + mon.Result
	}

	return fmt.Sprintf("%s · %s · %s", title, when, next)
}

// recentRows lists the builds panel's builds, newest first, in an area, below its columns' names. A selected
// monitor's summary heads them, a row for each of its first three lines. The summary leaves the builds a row, and
// ends in … where it is cut.
func (m model) recentRows(in area) []row {
	var rows []row
	if summary := m.pickedSummary(); summary != "" {
		lines := strings.Split(summary, "\n")
		kept := lines[:min(len(lines), 3, max(0, in.h-1))]
		for _, line := range kept {
			rows = append(rows, row{text: line})
		}

		if len(kept) > 0 && len(kept) < len(lines) {
			rows[len(rows)-1].text += m.theme.dim.Render(" …")
		}
	}

	in.h -= len(rows)
	return append(rows, m.buildRows(in)...)
}

// pickedSummary is the summary of the monitor selected in the specs panel, or "".
func (m model) pickedSummary() string {
	if s, ok := m.picked(); ok && s.Monitor != nil {
		return s.Monitor.Summary
	}

	return ""
}

// buildRows lists the builds panel's builds, newest first, in an area, below its columns' names.
func (m model) buildRows(in area) []row {
	all := m.recent()
	if len(all) == 0 {
		return []row{{text: m.theme.dim.Render("no build yet")}}
	}

	line, most := recentLine(in.w), 0.0
	for _, l := range all {
		cost, _ := known(l.b.Cost)
		most = max(most, cost)
	}

	// A spec's builds are told apart by number, and all specs' builds by spec.
	_, narrowed := m.picked()
	second := map[bool]string{true: "build", false: "spec"}[narrowed]
	// A panel with room for one build or two lists them with no names above.
	var rows []row
	if in.h > 2 {
		size, rounds := "size", "rounds"
		if m.side == monitorsPane {
			size, rounds = "", ""
		}

		rows = []row{{text: m.theme.dim.Render(line("started", second, "outcome", size, "time", "cost", "turns", "failed",
			rounds, ""))}}
	}

	sel := m.sel[recentPane]
	for i := max(0, sel-in.h+len(rows)+1); i < len(all) && len(rows) < in.h; i++ {
		cells := m.recentCells(all[i], most)
		if narrowed {
			cells[1] = fmt.Sprintf("#%d", all[i].build+1)
		}

		// A fix has no tests, and no rounds.
		if m.side == monitorsPane {
			cells[3], cells[8] = "", ""
		}

		rows = append(rows, row{m.theme.pick(line(cells...), m.focus == recentPane && i == sel),
			target{toRecent, i}})
	}

	return rows
}

// recentLine lays a build's cells out in w cells of the builds panel. A narrow panel drops the date, then the
// turns, failed calls and rounds. A wide one keeps the cost's bar.
func recentLine(w int) func(cells ...string) string {
	widths := []int{12, 16, 16, 8, -6, -7, -6, -7, -7}
	return func(cells ...string) string {
		switch {
		case w < 75:
			return cols([]int{12, 13, 8, -5, -7}, cells[1:6]...)
		case w < 93:
			return cols(widths, cells[:6]...)
		case w < 93+costBar:
			return cols(widths, cells[:9]...)
		}

		return cols(widths, cells...)
	}
}

// costBar is how many cells the widest cost's bar takes in the recent builds, with the space before it.
const costBar = 13

// recentCells are a build's cells in the builds panel. Its last is its cost as a bar, its share of most.
func (m model) recentCells(l listed, most float64) []string {
	took, tookKnown := known(l.b.Took)
	elapsed := "?"
	switch {
	case tookKnown:
		elapsed = span(took)
	case slices.ContainsFunc(l.b.runs(), (*Run).running):
		elapsed = span(m.now.Sub(l.b.Started))
	}

	cost, costKnown := known(l.b.Cost)
	turns, turnsKnown := known(l.b.Turns)
	failed, _, failedKnown := l.b.failed()
	dim := m.theme.dim
	return []string{dim.Render(l.b.Started.Local().Format("Jan 02 15:04")), l.spec, m.outcome(l.buildRef),
		plural(l.b.Size, "test"), elapsed, dollars(cost, costKnown), count(turns, turnsKnown), count(failed, failedKnown),
		count(known(l.b.Rounds)), dim.Render(gauge(cost, most, costBar-1))}
}

// gauge draws v's share of most as a bar of at most w cells, to an eighth of a cell.
func gauge(v, most float64, w int) string {
	if most <= 0 {
		return ""
	}

	eighths := int(v/most*float64(w*8) + 0.5)
	return strings.TrimRight(strings.Repeat("█", eighths/8)+string([]rune(" ▏▎▍▌▋▊▉")[eighths%8]), " ")
}

// outcome is how a build ended, in its color and beside its glyph. A spec's last build, not yet ended, shows where
// the spec stands. A fix is marked as its monitor would be.
func (m model) outcome(ref buildRef) string {
	s, said := NotEnded, NotEnded.String()
	spec := m.spec(ref.specRef)
	switch b := m.build(ref); {
	case b != nil && b.Ended != NotEnded:
		s, said = b.Ended, b.Outcome
	case ref.build == len(spec.Builds)-1:
		s = spec.State
		said = s.String()
	}

	style, glyph := m.theme.look(s)
	if glyph == "◌" {
		return said
	}

	return style.Render(glyph + " " + said)
}

// page is a screen's rows: its head, which stays, and its body, which scrolls. sel is the body's selected line, or
// -1.
func (m model) page(s *screen) (head, body []row, sel int) {
	switch s.kind {
	case buildScreen:
		return m.buildPage(s)
	case runScreen:
		return m.runPage(s)
	case textScreen:
		return m.textPage(s)
	case homeScreen:
		// The home screen draws its panes instead, so no caller asks for its page.
	}

	return nil, nil, -1
}

// textPage is the spec s shows, as approved for its build, under a head that says when.
func (m model) textPage(s *screen) (head, body []row, sel int) {
	b := m.build(s.buildRef)
	if b == nil {
		return []row{{text: s.spec}}, nil, -1
	}

	title := "as approved at " + short(b.Approved, 12)
	if b.Amendments != "" {
		title += ", with its amendments"
	}

	head = []row{{text: m.theme.dotted(m.theme.bold.Render(s.spec), title)}, {}}
	if s.drawn == nil {
		return head, []row{{text: m.theme.dim.Render("reading the spec")}}, -1
	}

	for _, line := range s.drawn {
		body = append(body, row{text: line})
	}

	return head, body, -1
}

func times(n int) string {
	if n == 1 {
		return "once"
	}

	return fmt.Sprintf("%d times", n)
}

func (m model) buildPage(s *screen) (head, body []row, sel int) {
	b := m.build(s.buildRef)
	if b == nil {
		return []row{{text: s.spec}}, nil, -1
	}

	t := m.theme
	head = []row{
		{text: t.dotted(t.bold.Render(s.spec), fmt.Sprintf("build %d", s.build+1),
			"approved at "+t.dim.Render(short(b.Approved, 12)), plural(b.Size, "test"), m.outcome(s.buildRef))},
		{text: m.summary(b)},
		{},
		{text: t.dim.Render(cols(m.runCols(), "", "run", "", "took", "turns", "cost", "failed", "context"))},
	}
	body, sel = m.stepRows(s, b)
	if b.Ended != NotEnded {
		body = append(body, row{text: cols(m.runCols()[:1], "", m.outcome(s.buildRef))})
	}

	return head, append(body, t.causes(b)...), sel
}

// stepRows are a build's runs and gates, in the order they ran, and the line of the selected run.
func (m model) stepRows(s *screen, b *Build) (body []row, sel int) {
	sel, n := -1, 0
	for _, step := range b.Steps {
		switch step := step.(type) {
		case *Run:
			if n == s.sel {
				sel = len(body)
			}

			body = append(body, row{m.theme.pick(m.runRow(n, step), n == s.sel), target{toRun, n}})
			body = append(body, m.theme.runNotes(step)...)
			n++
		case *Gate:
			body = append(body, row{text: cols(m.runCols(), "", m.theme.kiln.Render("gate"), m.theme.verdict(step),
				m.theme.dim.Render(span(step.End.Sub(step.Start))))})
		}
	}

	return body, sel
}

// runNotes say, below a run's row, that kiln stopped mid-run, and which paths it changed in the build's tree.
func (t theme) runNotes(r *Run) []row {
	var notes []row
	if r.Lost {
		notes = append(notes, row{text: "    " + t.kiln.Render("! kiln stopped mid-run")})
	}

	if len(r.Changed) > 0 {
		notes = append(notes, row{text: "    " + t.failed.Render("✗ changed the tree: "+strings.Join(r.Changed, ", "))})
	}

	return notes
}

// verdict says whether a gate passed, and whether it was the landing's. Its mark and color say whether the gate went
// as kiln wanted.
func (t theme) verdict(g *Gate) string {
	said := map[bool]string{true: "passed", false: "failed"}[g.Passed]
	if g.Passed == g.MustFail {
		said = t.failed.Render("✗ " + said)
	} else {
		said = t.passed.Render("✓ " + said)
	}

	if g.Landing {
		said += " · landing"
	}

	if g.MustFail {
		said += " · before the fix"
	}

	return said
}

// runCols are the widths of a build's columns. A narrow terminal narrows why each run started, down to 16 cells.
func (m model) runCols() []int {
	return []int{2, 6, max(16, 30-max(0, 86-m.w)), -7, -6, -8, -7, -13}
}

func (m model) runRow(n int, r *Run) string {
	took := "?"
	switch {
	case r.running():
		took = m.theme.agent.Render("▶ " + span(m.now.Sub(r.Start)))
	case !r.End.IsZero():
		took = span(r.End.Sub(r.Start))
	}

	turns, cost := "?", "?"
	if r.Totals.Turns != nil {
		turns = strconv.Itoa(*r.Totals.Turns)
	}

	if r.Totals.CostUSD != nil {
		cost = fmt.Sprintf("$%.2f", *r.Totals.CostUSD)
	}

	failed, context := "?", "?"
	if !r.End.IsZero() {
		failed = strconv.Itoa(len(r.Totals.Failed))
	}

	if len(r.Totals.Failed) > 0 {
		failed = m.theme.failed.Render(failed)
	}

	if c := r.Totals.Context; c != nil {
		context = tokens(c.Last)
	}

	return cols(m.runCols(), strconv.Itoa(n+1), m.theme.agent.Render(r.Role), cmp.Or(r.Why, "?"), took, turns, cost,
		failed, context)
}

// summary is a build's time, cost, turns and failed calls.
func (m model) summary(b *Build) string {
	took, tookKnown := known(b.Took)
	var gates, waiting time.Duration
	for _, s := range b.Steps {
		switch s := s.(type) {
		case *Gate:
			gates += s.End.Sub(s.Start)
		case *Run:
			waiting += s.Totals.Waiting
		}
	}

	elapsed := "time ?"
	if tookKnown {
		elapsed = "time " + span(took)
	}

	cost, costKnown := known(b.Cost)
	price := dollars(cost, costKnown)
	if !costKnown {
		price = "cost ?"
	}

	turns, turnsKnown := known(b.Turns)
	failed, refused, failedKnown := b.failed()
	return m.theme.dotted(fmt.Sprintf("%s: %s in gates, %s waiting on tools", elapsed, span(gates), span(waiting)),
		price, count(turns, turnsKnown)+" turns",
		fmt.Sprintf("%s failed, %s refused", count(failed, failedKnown), count(refused, failedKnown)))
}

// causes are a build's failed calls, grouped by cause, the most first.
func (t theme) causes(b *Build) []row {
	var failed []claude.Failure
	for _, r := range b.runs() {
		failed = append(failed, r.Totals.Failed...)
	}

	rows := []row{{}, {text: t.bold.Render("by cause")}}
	if len(failed) == 0 {
		return append(rows, row{text: t.dim.Render("no failed calls")})
	}

	for _, c := range claude.Causes(failed) {
		rows = append(rows, row{text: cols([]int{60}, t.failed.Render(fmt.Sprintf("✗ %3d  %s", c.Count, c.Text)),
			t.dim.Render(c.Tool+" "+c.Input))})
	}

	return rows
}

func (m model) runPage(s *screen) (head, body []row, sel int) {
	b := m.build(s.buildRef)
	runs := m.runs(s.buildRef)
	if b == nil || s.run >= len(runs) {
		return []row{{text: s.spec}}, nil, -1
	}

	r, t := runs[s.run], m.theme
	head = []row{
		{text: t.dotted(t.bold.Render(s.spec), fmt.Sprintf("build %d", s.build+1),
			fmt.Sprintf("run %d: %s", s.run+1, r.why()), t.agent.Render(r.Role)+" role",
			"session "+t.dim.Render(short(r.Session, 8)))},
		{text: m.runState(r, s.calls)},
		{},
	}
	if s.calls == nil {
		return head, []row{{text: t.dim.Render("reading its stream")}}, -1
	}

	for _, line := range s.drawn {
		body = append(body, row{text: line})
	}

	// One row, as callsFrom counts it.
	if len(body) > 0 {
		body = append(body, row{text: t.dim.Render("── calls, newest first " + strings.Repeat("─", max(0, m.w-23)))})
	}

	for _, a := range slices.Backward(s.calls.Actions) {
		body = append(body, row{text: m.action(a, r)})
	}

	if len(s.calls.Actions) == 0 {
		body = append(body, row{text: t.dim.Render("no call yet")})
	}

	return head, body, -1
}

// runState is how a run stands: how long it ran, how long since its session wrote, and its numbers.
func (m model) runState(r *Run, c *claude.Recounted) string {
	facts := m.ran(r, c)
	if c != nil {
		facts = append(facts, streamFacts(c)...)
	}

	turns, cost := "turns ?", "cost ?"
	if r.Totals.Turns != nil {
		turns = plural(*r.Totals.Turns, "turn")
	}

	if r.Totals.CostUSD != nil {
		cost = fmt.Sprintf("$%.2f", *r.Totals.CostUSD)
	}

	return m.theme.dotted(append(facts, turns, cost)...)
}

// ran is how long a run ran, or how long it has run and how long since its session wrote.
func (m model) ran(r *Run, c *claude.Recounted) []string {
	switch {
	case r.running():
		facts := []string{m.theme.agent.Render("▶ running " + long(m.now.Sub(r.Start)))}
		if c != nil {
			facts = append(facts, m.idle(c))
		}

		return facts
	case r.Lost:
		return []string{m.theme.kiln.Render("! kiln stopped mid-run")}
	}

	return []string{"took " + span(r.End.Sub(r.Start))}
}

// idle is how long since the session wrote. Past idleWarned with no call open, it asks for the developer's look.
func (m model) idle(c *claude.Recounted) string {
	idle := m.now.Sub(c.Written)
	said := "idle " + clock(idle)
	if idle > idleWarned && !slices.ContainsFunc(c.Actions, func(a claude.Action) bool { return a.Open }) {
		said = m.theme.you.Render(said)
	}

	return said
}

// streamFacts are what a run's stream counts: its context, its calls and its refusals.
func streamFacts(c *claude.Recounted) []string {
	var facts []string
	if ctx := c.Totals.Context; ctx != nil {
		facts = append(facts, "context "+tokens(ctx.Last))
	}

	calls := tally(c.Actions, func(a claude.Action) bool { return a.Tool != "" })
	return append(facts, plural(calls, "call"), fmt.Sprintf("%d refused", tally(c.Actions, isRefused)))
}

// tally counts the items that is reports true for.
func tally[T any](items []T, is func(T) bool) int {
	n := 0
	for _, a := range items {
		if is(a) {
			n++
		}
	}

	return n
}

func isRefused(a claude.Action) bool { return a.Refused }

// action is one call of a run, or one thing it said. Its input takes half the room the other columns leave.
func (m model) action(a claude.Action, r *Run) string {
	inputW := max(20, (m.w-34)/2)
	widths := []int{1, 8, 5, inputW, -6}
	t := m.theme
	at := t.dim.Render(a.At.Local().Format("15:04:05"))
	input := t.dim.Render(cut(a.Input, inputW))
	switch {
	case a.Tool == "":
		return cols(widths, "", at, "said", input)
	case a.Open && r.running():
		took := m.now.Sub(a.At)
		return t.agent.Render(cols(widths, "▶", a.At.Local().Format("15:04:05"), a.Tool, a.Input, clock(took),
			"running"))
	case a.Open:
		return cols(widths, t.failed.Render("✗"), at, a.Tool, input, "—", t.failed.Render("cut off"))
	case a.Refused:
		return cols(widths, t.failed.Render("✗"), at, a.Tool, input, "—", t.failed.Render("refused: "+a.Cause))
	case a.Failed:
		return cols(widths, t.failed.Render("✗"), at, a.Tool, input, seconds(a.Took), t.failed.Render(a.Cause))
	}

	return cols(widths, "", at, a.Tool, input, seconds(a.Took))
}

func seconds(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}

	return clock(d)
}

// long shows a running time to the second, such as 14m12s.
func long(d time.Duration) string {
	d = max(0, d.Round(time.Second))
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d/time.Second))
	}

	return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int(d%time.Minute/time.Second))
}

func short(s string, n int) string { return s[:min(n, len(s))] }
