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
	toSpec   = "spec"
	toNow    = "now"
	toRecent = "recent"
	toBuild  = "build"
	toRun    = "run"
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
	if err != nil || !slices.Contains([]string{toSpec, toNow, toRecent, toBuild, toRun, toPane}, to) {
		return target{}, false
	}

	return target{to, i}, true
}

// row is one line on screen. A row with a target is a layer of its own, which a click finds.
type row struct {
	text string
	to   target
}

var (
	red      = lipgloss.NewStyle().Foreground(lipgloss.Red)
	yellow   = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	green    = lipgloss.NewStyle().Foreground(lipgloss.Green)
	dim      = lipgloss.NewStyle().Faint(true)
	bold     = lipgloss.NewStyle().Bold(true)
	reversed = lipgloss.NewStyle().Reverse(true)
	frame    = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	// accent marks where the eye starts: the panel the keys go to, and a key's name. It marks what waits on the
	// developer too.
	accent  = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	heading = accent.Bold(true)
	blue    = lipgloss.NewStyle().Foreground(lipgloss.BrightBlue)
	magenta = lipgloss.NewStyle().Foreground(lipgloss.Magenta)
)

// dotted joins what a line says, with a dim dot between each two, so each reads apart.
func dotted(parts ...string) string { return strings.Join(parts, dim.Render(" · ")) }

// role colors a role's name, so a build session's runs and an audit's tell apart.
func role(name string) string {
	switch name {
	case "build":
		return blue.Render(name)
	case "audit":
		return magenta.Render(name)
	}

	return name
}

// layout places every row of the top screen in a terminal of m.w by m.h cells. The view draws it, and a click asks it
// which row it hit.
func (m model) layout() *lipgloss.Compositor {
	layers := []*lipgloss.Layer{lipgloss.NewLayer(strings.Repeat(strings.Repeat(" ", m.w)+"\n", m.h-1) +
		strings.Repeat(" ", m.w))}
	s := m.top()
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

// pick draws a selected row reversed, in no other color.
func pick(text string, selected bool) string {
	if selected {
		return reversed.Render(ansi.Strip(text))
	}

	return text
}

func (m model) foot(k kind) *lipgloss.Layer {
	text := footer(k, m.w-1)
	if m.err != nil {
		text = red.Render("✗ kiln could not read its ledger: " + m.err.Error())
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

// inside is the area within the panel's border.
func (p panel) inside() area { return area{p.w - 2, p.h - 2} }

// box draws the panel, with its rows inside it from the top.
func (p panel) box(rows []row) *lipgloss.Layer {
	if p.h < 2 {
		return lipgloss.NewLayer("").X(p.x).Y(p.y)
	}

	edge, name := frame, bold
	if p.lit {
		edge, name = accent, heading
	}

	inner := max(0, p.w-2)
	title := ansi.Truncate(p.title, max(0, inner-2), "…")
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
	sideW := m.sideWidth()
	rightW, h := m.w-sideW, m.h-1
	now := m.nowRows()
	tiles := m.trendRows(rightW - 2)
	nowH := len(now) + 2
	trendH := min(len(tiles)+2, max(3, h-nowH-5))
	recentH := max(0, h-nowH-trendH)
	specsBox := panel{to: target{toPane, int(specsPane)}, title: "specs", lit: m.focus == specsPane,
		area: area{sideW, h}}
	nowBox := panel{to: target{toPane, int(nowPane)}, title: "now", lit: m.focus == nowPane, x: sideW,
		area: area{rightW, nowH}}
	trendBox := panel{title: trendTitle(trend(m.specs)), x: sideW, y: nowH, area: area{rightW, trendH}}
	recentBox := panel{to: target{toPane, int(recentPane)}, title: "recent builds", lit: m.focus == recentPane,
		x: sideW, y: nowH + trendH, area: area{rightW, recentH}}
	return []*lipgloss.Layer{
		specsBox.box(m.side(specsBox.inside())),
		nowBox.box(now),
		trendBox.box(tiles),
		recentBox.box(m.recentRows(recentBox.inside())),
		m.foot(homeScreen),
	}
}

// sideWidth fits the specs panel to its longest row, but takes at most a third of the terminal.
func (m model) sideWidth() int {
	need := 0
	for _, s := range m.specs {
		need = max(need, ansi.StringWidth(s.Name)+len(s.State)+3)
	}

	return max(20, min(need+2, 40, m.w/3))
}

// side lists the specs whose names hold the filter, below the filter, in an area of the specs panel.
func (m model) side(in area) []row {
	filter := dim.Render("/ filter")
	switch {
	case m.typing:
		filter = "/ " + m.filter + "▏"
	case m.filter != "":
		filter = "/ " + m.filter
	}

	rows := []row{{text: filter}, {}}
	specs := m.filtered()
	if len(specs) == 0 {
		return append(rows, row{text: dim.Render("no spec")})
	}

	room := max(1, in.h-len(rows))
	sel := m.sel[specsPane]
	for i := max(0, sel-room+1); i < len(specs) && len(rows) < in.h; i++ {
		s := specs[i]
		text := marked(s.State) + " " + fit(s.Name, max(1, in.w-3-len(s.State))) + " " + stated(s.State)
		rows = append(rows, row{pick(text, m.focus == specsPane && i == sel), target{toSpec, i}})
	}

	return rows
}

// stated is a state's word. A spec that needs nothing more is dim, so the ones in play stand out.
func stated(s State) string {
	if s == Accepted || s == Rejected {
		return dim.Render(string(s))
	}

	return string(s)
}

// marked is a state's glyph, in its color.
func marked(s State) string {
	switch s {
	case Running:
		return yellow.Render("▶")
	case Paused:
		return yellow.Render("‖")
	case Halted:
		return yellow.Render("!")
	case Stopped:
		return red.Render("✗")
	case UAT, Waits:
		return heading.Render("◆")
	case Accepted:
		return green.Render("✓")
	case Rejected:
		return dim.Render("✗")
	}

	return "◌"
}

func (m model) nowRows() []row {
	ref, r, ok := running(m.specs)
	if i := slices.IndexFunc(m.specs, func(s Spec) bool { return s.State == Paused }); !ok && i >= 0 {
		return []row{{text: yellow.Render("‖ " + m.specs[i].Name + " · paused for a usage limit")}}
	}

	if !ok {
		return []row{{text: dim.Render("nothing is running")}}
	}

	first := dotted(yellow.Render("▶")+" "+bold.Render(ref.spec), role(r.Role)+" role", r.why(),
		"running "+span(m.now.Sub(r.Start)))
	second := dim.Render("reading its stream")
	if m.live != nil {
		second = strings.Join(m.liveFacts(r, m.live), dim.Render("  ·  "))
	}

	to := target{toNow, 0}
	return []row{{pick(first, m.focus == nowPane), to}, {second, to}}
}

// liveFacts are what a running run's stream says. They are how long since it was written, the call open, its
// refusals, and its context.
func (m model) liveFacts(r *Run, c *claude.Recounted) []string {
	idle := m.now.Sub(c.Written)
	open := slices.IndexFunc(c.Actions, func(a claude.Action) bool { return a.Open })
	said := "idle " + clock(idle)
	switch {
	case open >= 0:
		said += ", in a call: " + c.Actions[open].Input
	case idle > idleWarned:
		said = yellow.Render("▶ " + said + ", in no call")
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

// recentRows lists every build, newest first, in an area, below its columns' names. A narrow terminal drops the
// date, and a wide one draws each cost as a bar beside it.
func (m model) recentRows(in area) []row {
	all := m.recent()
	if len(all) == 0 {
		return []row{{text: dim.Render("no build yet")}}
	}

	line, most := recentLine(in.w), 0.0
	for _, l := range all {
		cost, _ := l.b.cost()
		most = max(most, cost)
	}

	// A panel with room for one build or two lists them with no names above.
	var rows []row
	if in.h > 2 {
		rows = []row{{text: dim.Render(line("started", "spec", "outcome", "size", "time", "cost", ""))}}
	}

	sel := m.sel[recentPane]
	for i := max(0, sel-in.h+len(rows)+1); i < len(all) && len(rows) < in.h; i++ {
		rows = append(rows, row{pick(line(m.recentCells(all[i], most)...), m.focus == recentPane && i == sel),
			target{toRecent, i}})
	}

	return rows
}

// recentLine lays a recent build's cells out in w cells. A narrow panel drops the date, and a wide one keeps the
// cost's bar.
func recentLine(w int) func(cells ...string) string {
	return func(cells ...string) string {
		switch {
		case w < 75:
			return cols([]int{12, 13, 8, -5, -7}, cells[1:6]...)
		case w < 75+costBar:
			return cols([]int{12, 16, 16, 8, -6, -7}, cells[:6]...)
		}

		return cols([]int{12, 16, 16, 8, -6, -7}, cells...)
	}
}

// costBar is how many cells the widest cost's bar takes in the recent builds, with the space before it.
const costBar = 13

// recentCells are a build's cells in the recent builds. Its last is its cost as a bar, its share of most.
func (m model) recentCells(l listed, most float64) []string {
	took, known := l.b.took()
	time := "?"
	switch {
	case known:
		time = span(took)
	case slices.ContainsFunc(l.b.runs(), (*Run).running):
		time = span(m.now.Sub(l.b.Started))
	}

	cost, costKnown := l.b.cost()
	return []string{dim.Render(l.b.Started.Local().Format("Jan 02 15:04")), l.spec, m.outcome(l.buildRef),
		plural(l.b.Size, "test"), time, dollars(cost, costKnown), dim.Render(gauge(cost, most, costBar-1))}
}

// gauge draws v's share of most as a bar of at most w cells, to an eighth of a cell.
func gauge(v, most float64, w int) string {
	if most <= 0 {
		return ""
	}

	eighths := int(v/most*float64(w*8) + 0.5)
	return strings.TrimRight(strings.Repeat("█", eighths/8)+string([]rune(" ▏▎▍▌▋▊▉")[eighths%8]), " ")
}

// outcome is how a build ended, in its color and beside its glyph. A build that has not ended shows where it stands.
func (m model) outcome(ref buildRef) string {
	var said string
	if b := m.build(ref); b != nil {
		said = b.Outcome
	}

	if said == "" {
		said = "not ended"
		if s := m.spec(ref.spec); ref.build == len(s.Builds)-1 {
			said = string(s.State)
		}
	}

	switch said {
	case "landed", "accepted":
		return green.Render("✓ " + said)
	case string(Stopped):
		return red.Render("✗ " + said)
	case string(Running), string(Paused):
		return yellow.Render("▶ " + said)
	}

	return said
}

// page is a screen's rows: its head, which stays, and its body, which scrolls. sel is the body's selected line, or
// -1.
func (m model) page(s *screen) (head, body []row, sel int) {
	switch s.kind {
	case specScreen:
		return m.specPage(s)
	case buildScreen:
		return m.buildPage(s)
	case runScreen:
		return m.runPage(s)
	}

	b := m.build(s.buildRef)
	if b == nil {
		return []row{{text: s.spec}}, nil, -1
	}

	title := "as approved at " + short(b.Approved, 12)
	if b.Amendments != "" {
		title += ", with its amendments"
	}

	head = []row{{text: dotted(heading.Render(s.spec), title)}, {}}
	if s.drawn == nil {
		return head, []row{{text: dim.Render("reading the spec")}}, -1
	}

	for _, line := range s.drawn {
		body = append(body, row{text: line})
	}

	return head, body, -1
}

func (m model) specPage(s *screen) (head, body []row, sel int) {
	spec := m.spec(s.spec)
	if spec == nil {
		return []row{{text: s.spec}, {}, {text: dim.Render("kiln holds no spec named " + s.spec)}}, nil, -1
	}

	// A narrow terminal drops the commit each build was approved at, which the build's screen shows.
	widths, narrow := []int{3, 12, 12, 22, -6, -9, -6, -7, -7}, m.w < 92
	line := func(cells ...string) string {
		if narrow {
			return cols(slices.Delete(slices.Clone(widths), 2, 3), slices.Delete(cells, 2, 3)...)
		}

		return cols(widths, cells...)
	}

	head = []row{
		{text: dotted(heading.Render(spec.Name), marked(spec.State)+" "+string(spec.State),
			"approved "+times(len(spec.Builds)))},
		{},
		{text: dim.Render(line("#", "started", "approved at", "outcome", "time", "cost", "turns", "failed", "rounds"))},
	}
	if len(spec.Builds) == 0 {
		return head, []row{{text: dim.Render("no build yet")}}, -1
	}

	// Newest first, while sel and each row's target stay the build's index, which a new build never moves.
	for i, b := range slices.Backward(spec.Builds) {
		took, known := b.took()
		time := "?"
		if known {
			time = span(took)
		}

		cost, costKnown := b.cost()
		turns, turnsKnown := b.turns()
		failed, _, failedKnown := b.failed()
		text := line(strconv.Itoa(i+1), dim.Render(b.Started.Local().Format("Jan 02 15:04")),
			dim.Render(short(b.Approved, 12)),
			m.outcome(buildRef{spec.Name, i}), time, dollars(cost, costKnown), count(turns, turnsKnown),
			count(failed, failedKnown), count(b.rounds()))
		body = append(body, row{pick(text, i == s.sel), target{toBuild, i}})
	}

	return head, body, len(spec.Builds) - 1 - s.sel
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

	head = []row{
		{text: dotted(heading.Render(s.spec), fmt.Sprintf("build %d", s.build+1),
			"approved at "+dim.Render(short(b.Approved, 12)), plural(b.Size, "test"), m.outcome(s.buildRef))},
		{text: m.summary(b)},
		{},
		{text: dim.Render(cols(m.runCols(), "", "run", "", "took", "turns", "cost", "failed", "context"))},
	}
	body, sel = m.stepRows(s, b)
	if b.Outcome != "" {
		body = append(body, row{text: cols(m.runCols()[:1], "", m.outcome(s.buildRef))})
	}

	return head, append(body, causes(b)...), sel
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

			body = append(body, row{pick(m.runRow(n, step), n == s.sel), target{toRun, n}})
			body = append(body, runNotes(step)...)
			n++
		case *Gate:
			body = append(body, row{text: cols(m.runCols(), "", dim.Render("gate"), verdict(step),
				dim.Render(span(step.End.Sub(step.Start))))})
		}
	}

	return body, sel
}

// runNotes say, below a run's row, that kiln stopped mid-run, and which paths it changed in the build's tree.
func runNotes(r *Run) []row {
	var notes []row
	if r.Lost {
		notes = append(notes, row{text: "    " + yellow.Render("! kiln stopped mid-run")})
	}

	if len(r.Changed) > 0 {
		notes = append(notes, row{text: "    " + red.Render("✗ changed the tree: "+strings.Join(r.Changed, ", "))})
	}

	return notes
}

// verdict says whether a gate passed, and whether it was the landing's.
func verdict(g *Gate) string {
	said := red.Render("✗ failed")
	if g.Passed {
		said = green.Render("✓ passed")
	}

	if g.Landing {
		said += " · landing"
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
		took = yellow.Render("▶ " + span(m.now.Sub(r.Start)))
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
		failed = red.Render(failed)
	}

	if c := r.Totals.Context; c != nil {
		context = tokens(c.Last)
	}

	return cols(m.runCols(), strconv.Itoa(n+1), role(r.Role), cmp.Or(r.Why, "?"), took, turns, cost, failed, context)
}

// summary is a build's time, cost, turns and failed calls.
func (m model) summary(b *Build) string {
	took, known := b.took()
	var gates, waiting time.Duration
	for _, s := range b.Steps {
		switch s := s.(type) {
		case *Gate:
			gates += s.End.Sub(s.Start)
		case *Run:
			waiting += s.Totals.Waiting
		}
	}

	time := "time ?"
	if known {
		time = "time " + span(took)
	}

	cost, costKnown := b.cost()
	price := dollars(cost, costKnown)
	if !costKnown {
		price = "cost ?"
	}

	turns, turnsKnown := b.turns()
	failed, refused, failedKnown := b.failed()
	return dotted(fmt.Sprintf("%s: %s in gates, %s waiting on tools", time, span(gates), span(waiting)), price,
		count(turns, turnsKnown)+" turns",
		fmt.Sprintf("%s failed, %s refused", count(failed, failedKnown), count(refused, failedKnown)))
}

// causes are a build's failed calls, grouped by cause, the most first.
func causes(b *Build) []row {
	var failed []claude.Failure
	for _, r := range b.runs() {
		failed = append(failed, r.Totals.Failed...)
	}

	rows := []row{{}, {text: bold.Render("by cause")}}
	if len(failed) == 0 {
		return append(rows, row{text: dim.Render("no failed calls")})
	}

	for _, c := range claude.Causes(failed) {
		rows = append(rows, row{text: cols([]int{60}, red.Render(fmt.Sprintf("✗ %3d  %s", c.Count, c.Text)),
			dim.Render(c.Tool+" "+c.Input))})
	}

	return rows
}

func (m model) runPage(s *screen) (head, body []row, sel int) {
	b := m.build(s.buildRef)
	runs := m.runs(s.buildRef)
	if b == nil || s.run >= len(runs) {
		return []row{{text: s.spec}}, nil, -1
	}

	r := runs[s.run]
	head = []row{
		{text: dotted(heading.Render(s.spec), fmt.Sprintf("build %d", s.build+1),
			fmt.Sprintf("run %d: %s", s.run+1, r.why()), role(r.Role)+" role",
			"session "+dim.Render(short(r.Session, 8)))},
		{text: m.runState(r, s.calls)},
		{},
	}
	if s.calls == nil {
		return head, []row{{text: dim.Render("reading its stream")}}, -1
	}

	for _, line := range s.drawn {
		body = append(body, row{text: line})
	}

	// One row, as callsFrom counts it.
	if len(body) > 0 {
		body = append(body, row{text: dim.Render("── calls, newest first " + strings.Repeat("─", max(0, m.w-23)))})
	}

	for _, a := range slices.Backward(s.calls.Actions) {
		body = append(body, row{text: m.action(a, r)})
	}

	if len(s.calls.Actions) == 0 {
		body = append(body, row{text: dim.Render("no call yet")})
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

	return dotted(append(facts, turns, cost)...)
}

// ran is how long a run ran, or how long it has run and how long since its session wrote.
func (m model) ran(r *Run, c *claude.Recounted) []string {
	switch {
	case r.running():
		facts := []string{yellow.Render("▶ running " + long(m.now.Sub(r.Start)))}
		if c != nil {
			facts = append(facts, m.idle(c))
		}

		return facts
	case r.Lost:
		return []string{yellow.Render("! kiln stopped mid-run")}
	}

	return []string{"took " + span(r.End.Sub(r.Start))}
}

// idle is how long since the session wrote. Past idleWarned with no call open, it is yellow.
func (m model) idle(c *claude.Recounted) string {
	idle := m.now.Sub(c.Written)
	said := "idle " + clock(idle)
	if idle > idleWarned && !slices.ContainsFunc(c.Actions, func(a claude.Action) bool { return a.Open }) {
		said = yellow.Render(said)
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

// tally counts the actions that is reports true for.
func tally(actions []claude.Action, is func(claude.Action) bool) int {
	n := 0
	for _, a := range actions {
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
	at := dim.Render(a.At.Local().Format("15:04:05"))
	input := dim.Render(cut(a.Input, inputW))
	switch {
	case a.Tool == "":
		return cols(widths, "", at, "said", input)
	case a.Open && r.running():
		took := m.now.Sub(a.At)
		return yellow.Render(cols(widths, "▶", a.At.Local().Format("15:04:05"), a.Tool, a.Input, clock(took),
			"running"))
	case a.Open:
		return cols(widths, red.Render("✗"), at, a.Tool, input, "—", red.Render("cut off"))
	case a.Refused:
		return cols(widths, red.Render("✗"), at, a.Tool, input, "—", red.Render("refused: "+a.Cause))
	case a.Failed:
		return cols(widths, red.Render("✗"), at, a.Tool, input, seconds(a.Took), red.Render(a.Cause))
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
