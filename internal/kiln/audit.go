package kiln

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// audit runs the audit agent on the directories the build changed, and its acceptance tests. It answers the findings,
// or "" for none. An audit that names no finding count is all findings. An audit that ends with no result runs again,
// in a new session, up to auditTries times. Once the audit answers, the record's Audited names the commit it read. A
// build that changed nothing to read records no read.
func (b *build) audit(ctx context.Context) (string, error) {
	head, dirs, files, err := b.changedFiles(ctx)
	if err != nil || len(dirs) == 0 {
		return "", err
	}

	b.say("the audit reads %s", strings.Join(dirs, " "))
	about, subject := auditing, b.rec.Name.file()
	switch {
	case b.fix != nil && b.rec.Accepted:
		about, subject = auditingLanding, string(b.rec.Name)
	case b.fix != nil:
		about, subject = auditingFix, string(b.rec.Name)
	}

	prompt := fmt.Sprintf(about, "`"+strings.Join(dirs, "`, `")+"`", subject, strings.Join(files, ", "))

	harness, err := b.auditedHarness(ctx)
	if err != nil {
		return "", err
	}

	if len(harness) > 0 {
		prompt += fmt.Sprintf(auditingHarness, strings.Join(harness, ", "))
	}

	// The audit's own worktree keeps what it writes out of the build's, even when kiln stops mid-audit.
	l, err := b.agentLaunch(ctx, auditRole, claude.Launch{Prompt: prompt, Dir: string(b.r.auditTree(b.rec.Name))})
	if err != nil {
		return "", err
	}

	findings, err := b.auditTries(ctx, l, head)
	if err == nil {
		b.set(func(r *record) { r.Audited = head })
	}

	// recount reads the worktree of an audit kiln stopped, so it stays until then.
	if ctx.Err() == nil {
		err = errors.Join(err, b.r.git.Remove(ctx, []git.Worktree{git.Worktree(l.Dir)}))
	}

	return findings, err
}

// auditTries runs the audit l launches on head until it ends with a result, up to auditTries times.
func (b *build) auditTries(ctx context.Context, l claude.Launch, head git.Rev) (string, error) {
	for try := 1; ; try++ {
		findings, err := b.auditTry(ctx, l, head, try)
		if !errors.Is(err, errNoResult) {
			return findings, err
		}

		b.say("the audit ended with no result; it runs again")
	}
}

// errNoResult is an audit run that ended with no result, with tries left.
var errNoResult = errors.New("the audit ended with no result")

// auditTry runs the audit l launches, in a new session, on the build's commit head. It answers its findings, or
// errNoResult for a try that ended with no result before the last.
func (b *build) auditTry(ctx context.Context, l claude.Launch, head git.Rev, try int) (string, error) {
	if err := b.r.git.Detached(ctx, git.Worktree(l.Dir), head); err != nil {
		return "", err
	}

	b.auditing = head
	l.Session = claude.NewID()
	ended, err := b.talk(ctx, l, reason{Why: whyAudit, Try: try})
	switch {
	case err != nil:
		return "", err
	case !ended.Failed:
		rep := readReply(ended.Text)
		if !rep.parsed {
			b.say("kiln could not parse the audit's reply, so its ledger holds the reply whole")
		}

		return rep.back(), nil
	case try < auditTries:
		return "", errNoResult
	}

	return "", fmt.Errorf("the audit ended with no result %d times", auditTries)
}

// reply is an audit's reply, read in the form kiln's contract with the audit sets, which no agent's file changes.
type reply struct {
	text string
	// tally is nil for a reply whose last line names no tally.
	tally *tally
	// findings are those the reply lists, when parsed.
	findings []finding
	// parsed is whether the reply names a tally, and lists in the contract's form the findings it reports.
	parsed bool
}

// tally counts an audit's findings: those it found, and those it listed, which a cap may cut.
type tally struct {
	Found    int `json:"found"`
	Reported int `json:"reported"`
}

// finding is one finding an audit's reply lists, as a finding line of the ledger holds it. A line for a reply kiln
// could not parse holds that reply in Reply alone.
type finding struct {
	// Rank is the finding's number in the reply, from 1.
	Rank int    `json:"rank,omitempty"`
	Path string `json:"path,omitempty"`
	// Line is 0 for a finding that names a whole file.
	Line     int    `json:"line,omitempty"`
	Category string `json:"category,omitempty"`
	Reaches  string `json:"reaches,omitempty"`
	Chance   string `json:"chance,omitempty"`
	Harm     string `json:"harm,omitempty"`
	Design   string `json:"design,omitempty"`
	Patch    string `json:"patch,omitempty"`
	Proof    string `json:"proof,omitempty"`
	Reply    string `json:"reply,omitempty"`
}

var (
	tallyLine   = regexp.MustCompile(`(?m)^.*found (\d+), reported (\d+)\s*\z`)
	findingHead = regexp.MustCompile("^(\\d+)\\.\\s+`?([^`\\s]+?)(?::(\\d+)(?:-\\d+)?)?`?\\s+" +
		"(bug|impossible by design|weak test|false comment|test-only export)$")
	findingField = regexp.MustCompile(`^(reaches|chance|harm|design|patch|proof)\s+(\S.*)$`)
)

// readReply reads an audit's reply text.
func readReply(text string) reply {
	r := reply{text: strings.TrimSpace(text)}
	body := r.text
	if m := tallyLine.FindStringSubmatchIndex(r.text); m != nil {
		found, _ := strconv.Atoi(r.text[m[2]:m[3]])
		reported, _ := strconv.Atoi(r.text[m[4]:m[5]])
		r.tally, body = &tally{Found: found, Reported: reported}, r.text[:m[0]]
	}

	findings, listed := listed(body)
	r.parsed = listed && r.tally != nil && r.tally.Reported <= r.tally.Found && len(findings) == r.tally.Reported
	if r.parsed {
		r.findings = findings
	}

	return r
}

// listed answers the findings body lists, and whether each is in the contract's form, numbered from 1 in turn. A
// finding is its head line, then its fields up to a blank line or a fence. A line that starts no field runs on from
// the field above it. Lines outside a finding are the reply's own prose.
func listed(body string) ([]finding, bool) {
	var findings []finding
	var open *finding
	var field *string
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(line)
		head := findingHead.FindStringSubmatch(line)
		switch {
		case head != nil:
			if open != nil && !held(open) {
				return nil, false
			}

			findings = append(findings, finding{Path: head[2], Category: head[4]})
			open, field = &findings[len(findings)-1], nil
			open.Rank, _ = strconv.Atoi(head[1])
			open.Line, _ = strconv.Atoi(head[3])
			if open.Rank != len(findings) {
				return nil, false
			}
		case open == nil:
		case line == "" || strings.HasPrefix(line, "```"):
			if !held(open) {
				return nil, false
			}

			open = nil
		default:
			if field = fieldOf(open, line, field); field == nil {
				return nil, false
			}
		}
	}

	return findings, open == nil || held(open)
}

// held is whether f holds a field, which a finding in the contract's form does.
func held(f *finding) bool {
	return f.Reaches+f.Chance+f.Harm+f.Design+f.Patch+f.Proof != ""
}

// fieldOf adds line to f, and answers the field it went in. A line that names a field starts it, unless f holds it
// already. Any other line runs on from last, the field before it. It answers nil for a line it can't place.
func fieldOf(f *finding, line string, last *string) *string {
	m := findingField.FindStringSubmatch(line)
	if m == nil {
		if last != nil {
			*last += " " + line
		}

		return last
	}

	field := map[string]*string{"reaches": &f.Reaches, "chance": &f.Chance, "harm": &f.Harm, "design": &f.Design,
		"patch": &f.Patch, "proof": &f.Proof}[m[1]]
	if *field != "" {
		return nil
	}

	*field = m[2]
	return field
}

// heardAudit answers the tally an audit run's reply ends with, and its finding lines. A run that ended with no result
// has neither.
func heardAudit(ended claude.Ended) (*tally, []finding) {
	if ended.Failed {
		return nil, nil
	}

	r := readReply(ended.Text)
	return r.tally, r.lines()
}

// back is what goes back to the build: "" when r's tally found nothing, or else the whole reply. A reply that names
// no tally is all findings.
func (r reply) back() string {
	if r.tally != nil && r.tally.Found == 0 {
		return ""
	}

	return r.text
}

// lines are r's finding lines for the ledger. A parsed reply has one per finding. Another has one that holds it whole.
func (r reply) lines() []finding {
	if r.parsed {
		return r.findings
	}

	return []finding{{Reply: r.text}}
}

// changedFiles answers the build's commit, and the files its work changed since it left main, outside .kiln/. In
// an accepted fix's landing, they are the files changed since kiln accept, as changedSince lists them. It answers
// their directories too, as changedDirs names them.
func (b *build) changedFiles(ctx context.Context) (head git.Rev, dirs, files []string, err error) {
	head, err = b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", nil, nil, err
	}

	changed, err := b.ownFiles(ctx, head)
	if err != nil {
		return "", nil, nil, err
	}

	files = slices.DeleteFunc(changed, func(p string) bool { return !outsideKiln(p) })
	dirs, err = b.changedDirs(ctx, head, files)
	return head, dirs, files, err
}

// ownFiles lists the files the work at head changed since it left main. An accepted fix's landing lists those changed
// since kiln accept.
func (b *build) ownFiles(ctx context.Context, head git.Rev) ([]string, error) {
	if b.fix != nil && b.rec.Accepted {
		return b.r.changedSince(ctx, b.rec.AcceptedAt, head)
	}

	main, err := b.r.mainTip(ctx)
	if err != nil {
		return nil, err
	}

	return b.r.git.Branched(ctx, main, head)
}

// changedDirs are the directories at head that hold paths, sorted, each once: . for the top, or the directory's path.
// A directory the work emptied gives way to the nearest one above it that head holds. A deletion is audited too.
// It names no file extension, so it serves a project in any language.
func (b *build) changedDirs(ctx context.Context, head git.Rev, paths []string) ([]string, error) {
	var dirs []string
	for _, p := range paths {
		dir, err := b.heldDir(ctx, head, path.Dir(p))
		if err != nil {
			return nil, err
		}

		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}

	slices.Sort(dirs)
	return dirs, nil
}

// heldDir is dir, or the nearest directory above it, that holds a file at head. The top always counts.
func (b *build) heldDir(ctx context.Context, head git.Rev, dir string) (string, error) {
	for ; dir != "."; dir = path.Dir(dir) {
		files, err := b.r.git.Files(ctx, head, dir)
		if err != nil || len(files) > 0 {
			return dir, err
		}
	}

	return ".", nil
}

const auditTries = 3

// auditedHarness lists each harness file the build session changed, for the audit. A fix has no split sides.
func (b *build) auditedHarness(ctx context.Context) ([]string, error) {
	if b.fix != nil {
		return nil, nil
	}

	return b.harnessChanged(ctx)
}
