package claude

import (
	"bufio"
	"cmp"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Totals is what one run of a session printed, counted. A run is one start or resume of the session. A nil field is
// a number the run printed nothing to read it from: unknown, not zero.
type Totals struct {
	// Turns and CostUSD sum the run's result events. When a run resumes the session, the run's result event
	// counts the turns and cost of that run alone.
	Turns   *int     `json:"turns,omitempty"`
	CostUSD *float64 `json:"cost_usd,omitempty"`
	// Context is the input tokens of the run's top-level messages.
	Context *Context `json:"context,omitempty"`
	// SevenDay is the use of the 7-day usage limit, from 0 to 1.
	SevenDay    *Use `json:"seven_day,omitempty"`
	Compactions int  `json:"compactions,omitempty"`
	// Tools counts the calls of each tool, by its name, the subagents' calls among them.
	Tools map[string]int `json:"tools,omitempty"`
	// Waiting is the time in which at least one top-level call but Agent had not yet returned. Subagents is the same
	// for Agent calls.
	Waiting   time.Duration `json:"waiting,omitempty"`
	Subagents time.Duration `json:"subagents,omitempty"`
	// Slowest are the run's slowest top-level calls but Agent, longest first, at most 5 of them.
	Slowest []Call `json:"slowest,omitempty"`
	// Failed are the calls refused, or that returned an error, in the order they failed.
	Failed []Failure `json:"failed,omitempty"`
}

// Context is a count of input tokens: input, cache-read and cache-creation tokens, summed.
type Context struct {
	First int `json:"first"`
	Peak  int `json:"peak"`
	Last  int `json:"last"`
}

// Use is the use of a usage limit at the run's first and last reading, from 0 to 1.
type Use struct {
	First float64 `json:"first"`
	Last  float64 `json:"last"`
}

// Call is one tool call that returned.
type Call struct {
	Tool string `json:"tool"`
	// Input is the first line of what the call was given, such as a Bash command or a file's path.
	Input string        `json:"input"`
	Took  time.Duration `json:"took"`
}

// Failure is one tool call refused, or that returned an error.
type Failure struct {
	Tool  string `json:"tool"`
	Input string `json:"input"`
	// Cause groups failures. A refusal's cause is its reason. An error's is the first line of its output that says
	// something, with any leading path:line: dropped.
	Cause   string `json:"cause"`
	Refused bool   `json:"refused,omitempty"`
}

const (
	slowestKept = 5
	// lineKept is how much of a call's input, or of its cause, Totals keeps.
	lineKept = 200
)

// Streamed answers the length in bytes of the session's stream, across its runs so far.
func (c Claude) Streamed(session string) (int64, error) {
	info, err := os.Stat(c.stream(session))
	if os.IsNotExist(err) {
		return 0, nil
	}

	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

// Recounted is a stretch of a session's stream, counted again.
type Recounted struct {
	Totals Totals
	// Actions are the top-level tool calls the stretch holds, and the text the model said, in order.
	Actions []Action
	// Written is when the stream was last written, across all its runs.
	Written time.Time
	// Answer is the text of the stretch's last result event: what the session answered. It is "" until the session
	// writes one. Failed is true of a stretch with no result, or an error as its result, as for Ended.
	Answer string
	Failed bool
}

// Action is one tool call, or one text the model said.
type Action struct {
	At time.Time
	// Tool is the tool called, or "" for text the model said.
	Tool string
	// Input is the first line of the call's input, or of the text.
	Input string
	// Took is how long a call took to return.
	Took time.Duration
	// Open is a call that has not returned.
	Open bool
	// Failed is a call that returned an error or was refused. Cause is as a [Failure]'s.
	Failed  bool
	Refused bool
	Cause   string
}

// Recount totals the session's stream from byte from up to byte to, as Run totals a run it reads. A to below 0
// reads to the end. kiln recounts a run it lost track of, such as when it died mid-run. kiln's view
// recounts a run to show it.
func (c Claude) Recount(session string, from, to int64) (Recounted, error) {
	f, err := os.Open(c.stream(session))
	if os.IsNotExist(err) {
		return Recounted{}, nil
	}

	if err != nil {
		return Recounted{}, err
	}

	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return Recounted{}, err
	}

	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return Recounted{}, err
	}

	var stretch io.Reader = f
	if to >= 0 {
		stretch = io.LimitReader(f, max(0, to-from))
	}

	var read reading
	lines := bufio.NewScanner(stretch)
	lines.Buffer(nil, 64<<20)
	for lines.Scan() {
		read.line(lines.Bytes())
	}

	ended := read.ended()
	counted := Recounted{Totals: ended.Totals, Actions: read.count.actions, Written: info.ModTime(), Answer: ended.Text,
		Failed: ended.Failed}

	return counted, lines.Err()
}

func (c Claude) stream(session string) string {
	return filepath.Join(c.c.Sessions, session, streamFile)
}

// tally counts a run's lines as they pass.
type tally struct {
	results, priced int
	turns           int
	cost            float64
	// messages are the top-level messages' IDs, in order, and contexts their tokens.
	messages    []string
	contexts    map[string]int
	sevenDay    *Use
	compactions int
	tools       map[string]int
	calls       map[string]*pending
	waits, subs []span
	slowest     []Call
	failed      []Failure
	// refused maps a refused call's ID to its index in failed.
	refused map[string]int
	actions []Action
	// acted maps a top-level call's ID to its index in actions.
	acted map[string]int
}

type pending struct {
	tool, input string
	top         bool
	at          time.Time
}

type span struct{ from, to time.Time }

// event is the part of a stream's line that a tally reads.
type event struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	Timestamp string          `json:"timestamp"`
	Parent    *string         `json:"parent_tool_use_id"`
	Message   json.RawMessage `json:"message"`
	// A result's fields.
	Turns     int `json:"num_turns"`
	SinceLast map[string]struct {
		Cost float64 `json:"costUSD"`
	} `json:"model_usage_since_last_result"`
	// A rate_limit_event's.
	Limit struct {
		Windows struct {
			SevenDay *struct {
				Utilization float64 `json:"utilization"`
			} `json:"seven_day"`
		} `json:"unifiedWindows"`
	} `json:"rate_limit_info"`
	// A permission_denied event's.
	ToolName  string `json:"tool_name"`
	ToolUseID string `json:"tool_use_id"`
}

type message struct {
	ID      string  `json:"id"`
	Content []block `json:"content"`
	Usage   struct {
		Input         int `json:"input_tokens"`
		CacheRead     int `json:"cache_read_input_tokens"`
		CacheCreation int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Text      string          `json:"text"`
}

func (t *tally) line(line []byte) {
	var e event
	if json.Unmarshal(line, &e) != nil {
		return
	}

	switch e.Type {
	case "assistant", "user":
		t.message(e)
	case "result":
		t.result(e)
	case "rate_limit_event":
		t.rateLimit(e)
	case "system":
		t.system(e)
	}
}

func (t *tally) message(e event) {
	var m message
	// A user's prompt is a string, not blocks, and holds nothing a tally counts.
	if json.Unmarshal(e.Message, &m) != nil {
		return
	}

	top := e.Parent == nil
	if top && e.Type == "assistant" {
		t.context(m)
	}

	at, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
	for _, b := range m.Content {
		t.block(b, top, at)
	}
}

func (t *tally) result(e event) {
	t.results++
	t.turns += e.Turns
	if e.SinceLast == nil {
		return
	}

	t.priced++
	for _, usage := range e.SinceLast {
		t.cost += usage.Cost
	}
}

func (t *tally) rateLimit(e event) {
	w := e.Limit.Windows.SevenDay
	if w == nil {
		return
	}

	if t.sevenDay == nil {
		t.sevenDay = &Use{First: w.Utilization}
	}

	t.sevenDay.Last = w.Utilization
}

func (t *tally) system(e event) {
	switch e.Subtype {
	case "compact_boundary":
		t.compactions++
	case "permission_denied":
		var reason string
		_ = json.Unmarshal(e.Message, &reason)
		f := Failure{Tool: e.ToolName, Cause: clip(reason), Refused: true}
		if c := t.calls[e.ToolUseID]; c != nil {
			f.Input = c.input
		}

		if i, ok := t.acted[e.ToolUseID]; ok {
			t.actions[i].Failed, t.actions[i].Refused, t.actions[i].Cause = true, true, f.Cause
		}

		if t.refused == nil {
			t.refused = map[string]int{}
		}

		if i, ok := t.refused[e.ToolUseID]; ok {
			t.failed[i] = f
			return
		}

		t.refused[e.ToolUseID] = len(t.failed)
		t.failed = append(t.failed, f)
	}
}

func (t *tally) context(m message) {
	if m.ID == "" {
		return
	}

	if t.contexts == nil {
		t.contexts = map[string]int{}
	}

	if _, seen := t.contexts[m.ID]; !seen {
		t.messages = append(t.messages, m.ID)
	}

	t.contexts[m.ID] = m.Usage.Input + m.Usage.CacheRead + m.Usage.CacheCreation
}

func (t *tally) block(b block, top bool, at time.Time) {
	switch b.Type {
	case "tool_use":
		t.use(b, top, at)
	case "text":
		if said, _, _ := strings.Cut(strings.TrimSpace(b.Text), "\n"); top && said != "" {
			t.actions = append(t.actions, Action{At: at, Input: clip(said)})
		}
	case "tool_result":
		t.toolResult(b, at)
	}
}

func (t *tally) use(b block, top bool, at time.Time) {
	if t.calls == nil {
		t.calls, t.tools = map[string]*pending{}, map[string]int{}
	}

	if _, seen := t.calls[b.ID]; seen {
		return
	}

	t.calls[b.ID] = &pending{tool: b.Name, input: firstLine(b.Input), top: top, at: at}
	t.tools[b.Name]++
	if top {
		t.act(b.ID, Action{At: at, Tool: b.Name, Input: t.calls[b.ID].input, Open: true})
	}
}

func (t *tally) toolResult(b block, at time.Time) {
	c := t.calls[b.ToolUseID]
	if b.IsError {
		t.fail(b, c)
	}

	t.returned(b, at)
	if c.timed(at) {
		t.took(c, at)
	}
}

// timed says whether c is a top-level call whose wait, from its start to at, can be measured.
func (c *pending) timed(at time.Time) bool {
	return c != nil && c.top && !c.at.IsZero() && !at.Before(c.at)
}

// took keeps how long the call c waited, returning at.
func (t *tally) took(c *pending, at time.Time) {
	s := span{c.at, at}
	if c.tool == "Agent" {
		t.subs = append(t.subs, s)
		return
	}

	t.waits = append(t.waits, s)
	t.slowest = append(t.slowest, Call{Tool: c.tool, Input: c.input, Took: at.Sub(c.at)})
}

func (t *tally) act(id string, a Action) {
	if t.acted == nil {
		t.acted = map[string]int{}
	}

	t.acted[id] = len(t.actions)
	t.actions = append(t.actions, a)
}

// returned marks a top-level call as returned, and failed if its result is an error.
func (t *tally) returned(b block, at time.Time) {
	i, ok := t.acted[b.ToolUseID]
	if !ok {
		return
	}

	a := &t.actions[i]
	a.Open = false
	if !a.At.IsZero() && !at.Before(a.At) {
		a.Took = at.Sub(a.At)
	}

	if b.IsError && !a.Refused {
		a.Failed, a.Cause = true, cause(text(b.Content))
	}
}

// fail keeps a call that returned an error, unless it was refused, which its permission_denied event keeps.
func (t *tally) fail(b block, c *pending) {
	if i, ok := t.refused[b.ToolUseID]; ok {
		if c != nil {
			t.failed[i].Input = c.input
		}

		return
	}

	f := Failure{Cause: cause(text(b.Content))}
	if c != nil {
		f.Tool, f.Input = c.tool, c.input
	}

	t.failed = append(t.failed, f)
}

func (t *tally) totals() Totals {
	tt := Totals{
		SevenDay:    t.sevenDay,
		Compactions: t.compactions,
		Tools:       t.tools,
		Waiting:     union(t.waits),
		Subagents:   union(t.subs),
		Failed:      t.failed,
	}
	if t.results > 0 {
		tt.Turns = &t.turns
	}

	if t.results > 0 && t.priced == t.results {
		tt.CostUSD = &t.cost
	}

	if len(t.messages) > 0 {
		c := Context{First: t.contexts[t.messages[0]], Last: t.contexts[t.messages[len(t.messages)-1]]}
		for _, id := range t.messages {
			c.Peak = max(c.Peak, t.contexts[id])
		}

		tt.Context = &c
	}

	slowest := slices.Clone(t.slowest)
	slices.SortStableFunc(slowest, func(a, b Call) int { return cmp.Compare(b.Took, a.Took) })
	if len(slowest) > 0 {
		tt.Slowest = slowest[:min(slowestKept, len(slowest))]
	}

	return tt
}

// Cause is the failed calls of one cause: how many, and the tool and input of the first.
type Cause struct {
	// Text names the cause. For a refusal, Text begins with "refused:" and a space.
	Text        string
	Tool, Input string
	Count       int
}

// Causes groups failures by cause, the most first.
func Causes(failed []Failure) []Cause {
	var all []Cause
	for _, f := range failed {
		text := cmp.Or(f.Cause, "(no output)")
		if f.Refused {
			text = "refused: " + text
		}

		i := slices.IndexFunc(all, func(c Cause) bool { return c.Text == text })
		if i < 0 {
			i = len(all)
			all = append(all, Cause{Text: text, Tool: f.Tool, Input: f.Input})
		}

		all[i].Count++
	}

	slices.SortStableFunc(all, func(a, b Cause) int { return cmp.Compare(b.Count, a.Count) })
	return all
}

// union is the time that at least one of spans covers.
func union(spans []span) time.Duration {
	spans = slices.Clone(spans)
	slices.SortFunc(spans, func(a, b span) int { return a.from.Compare(b.from) })
	var total time.Duration
	var end time.Time
	for _, s := range spans {
		if s.from.After(end) {
			end = s.from
		}

		if s.to.After(end) {
			total += s.to.Sub(end)
			end = s.to
		}
	}

	return total
}

// firstLine is the first line of a call's input. It reads the field that names what the call does, or else the
// whole input.
func firstLine(input json.RawMessage) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(input, &fields) == nil {
		for _, key := range []string{"command", "file_path", "notebook_path", "pattern", "url", "description"} {
			var s string
			if json.Unmarshal(fields[key], &s) == nil && s != "" {
				line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
				return clip(line)
			}
		}
	}

	line, _, _ := strings.Cut(string(input), "\n")
	return clip(line)
}

// text reads a tool result's content, a string or a list of text blocks.
func text(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}

	var blocks []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(content, &blocks)
	var b strings.Builder
	for _, block := range blocks {
		b.WriteString(block.Text)
		b.WriteString("\n")
	}

	return b.String()
}

var (
	exitStatus    = regexp.MustCompile(`^(Exit code|exit status) -?\d+$`)
	packageHeader = regexp.MustCompile(`^# \S+$`)
	position      = regexp.MustCompile(`^[^\s:]+:\d+:(\d+:)?\s*`)
)

// cause is the first line of an error's output that says something. It skips an exit status and a Go package
// header, and drops a leading path:line: or path:line:col:.
func cause(output string) string {
	for line := range strings.Lines(output) {
		line = strings.TrimSpace(line)
		if says(line) {
			return clip(position.ReplaceAllString(line, ""))
		}
	}

	return ""
}

// says is false of a blank line, an exit status and a Go package header.
func says(line string) bool {
	return line != "" && !exitStatus.MatchString(line) && !packageHeader.MatchString(line)
}

func clip(s string) string {
	if len(s) <= lineKept {
		return s
	}

	cut := lineKept
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut] + "..."
}

// Stderr is the end of what the session wrote to its standard error, in its last run. It is "" when kiln kept none.
func (c Claude) Stderr(session string) string {
	raw, _ := os.ReadFile(filepath.Join(c.c.Sessions, session, stderrFile))
	return string(raw)
}
