package crash_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// leftover is what a kiln killed now would leave of a flow's subject, outside the files of its worktrees.
type leftover struct {
	// stands is the subject's record, less its data: where the build stands, and what kiln was in the middle of.
	stands string
	// decides adds what the sessions, main and the subject's branch show of it. The next kiln decides by those.
	decides string
	// class adds which of the subject's checkouts and branches are there, for the next kiln to make or remove.
	class string
	// lost names each run with no end, which the next kiln ends whatever the record holds. roles are their roles.
	lost, roles []string
	// broken lists each rule the leftover breaks, whatever command comes next.
	broken []string
}

// data are the fields of a record that hold what its build has learned. The rest say where the build stands, and
// what kiln was in the middle of.
var data = []string{"name", "approved", "build", "pid", "paused", "limit", "audited_at", "accepted_at", "landed_at",
	"told", "old_tests", "title", "tested", "proof", "proved", "failing_at_main", "amendments", "why", "failing"}

// keys are what a set of leftovers holds l by: each lost run, and what the next kiln decides by. With byClass, the
// class is one too.
func (l leftover) keys(byClass bool) []string {
	keys := append(slices.Clone(l.lost), l.decides)
	if byClass {
		keys = append(keys, l.class)
	}

	return keys
}

// holds reads what a kill at the step st would leave, as left does. It fails the test for each rule that breaks.
//
// A session prints while kiln takes its next steps, and a kiln runs two sessions at once. So what a kill leaves of a
// session would turn on the machine's load. Each step first waits until the sessions kiln runs have begun. running
// counts them. So a kill between two sessions' starts leaves the first begun, and the second not.
//
// A snapshot copies the scratch file by file, while each session prints on. A session that starts during the copy
// can leave its stream there, yet not its transcript, which no kill leaves. The wait keeps each start out of the copy.
func (s *scratch) holds(f flow, st step, running int) leftover {
	for asked := time.Now(); s.begun() < running; time.Sleep(time.Millisecond) {
		if time.Since(asked) > beginWait {
			s.t.Errorf("%s: at step %d, %s, a session kiln started printed nothing in %s, so a snapshot could "+
				"hold half of its start", f.name, st.n, st.what, beginWait)
			break
		}
	}

	l := s.left(f)
	for _, rule := range l.broken {
		s.t.Errorf("%s: a kill at step %d, %s, would leave a rule broken: %s", f.name, st.n, st.what, rule)
	}

	return l
}

// left reads what a kiln killed now would leave of the flow's subject. Each rule it holds is true whatever command
// comes next:
//
//   - A build the ledger holds with no outcome has a record, so the next kiln ends it.
//   - A run with no end is of a record left building, so the next kiln ends the run as it takes the record up.
//   - An audit run that ended with a result holds a tally.
//   - No worktree, clone or branch of the subject is left once the book holds no record of it.
//   - Main holds the first of the flow's landings, each once, and never the file the audit wrote.
//   - Main moves only to the commit that the subject's record named as its landing before the move.
//   - The ledger holds each line it held when left last read it, in its place.
func (s *scratch) left(f flow) leftover {
	rec, lines := s.kept()
	refs := s.refs()
	builds, runs, broken := unended(lines)
	broken = slices.Concat(broken, s.moved(rec, refs["refs/heads/main"]), s.shrunk(lines))
	l := leftover{stands: stands(rec), broken: broken}
	for build := range builds {
		if rec["build"] != build {
			l.broken = append(l.broken, "the ledger holds a build with no outcome, and the book no record: "+build)
		}
	}

	for session, start := range runs {
		begun := s.printed(session, start)
		l.roles = append(l.roles, start.Role)
		l.lost = append(l.lost, fmt.Sprintf("a run of the %s session with no end, begun: %v", start.Role, begun))
		if rec["build"] != start.Build || rec["state"] != "building" {
			l.broken = append(l.broken, "the ledger holds a run with no end, and the book no build at work: "+session)
		}
	}

	slices.Sort(l.roles)
	slices.Sort(l.lost)
	there := s.checkouts(refs)
	for _, checkout := range there {
		if rec == nil {
			l.broken = append(l.broken, "the book holds no record of "+string(s.subject)+", yet "+checkout+" is left")
		}
	}

	l.broken = append(l.broken, s.mainBroken(f, refs)...)
	shows := slices.Concat([]string{l.stands}, s.shows(rec, refs), s.amended(refs))
	l.decides = strings.Join(shows, ", ")
	l.class = strings.Join(append(shows, there...), ", ")
	return l
}

// ledgerLine is a line of the ledger, as kiln writes it.
type ledgerLine struct {
	Kind, Build, Session, Role, Ended, Found string
	Tally                                    *json.RawMessage
	Streamed                                 int64
	// text is the line as the ledger holds it.
	text string
}

// beginWait is how long a step waits for the sessions kiln runs to begin. Under load, a scripted session takes
// seconds to print. A kiln waits a minute for a step's answer, so the wait is shorter.
const beginWait = 30 * time.Second

// begun counts the runs with no end whose session has printed since the run started.
func (s *scratch) begun() int {
	_, lines := s.kept()
	_, runs, _ := unended(lines)
	begun := 0
	for session, start := range runs {
		if s.printed(session, start) {
			begun++
		}
	}

	return begun
}

// printed is whether session has printed since start, the start line of its run.
func (s *scratch) printed(session string, start ledgerLine) bool {
	return s.streamed(session) > start.Streamed
}

// kept reads the record of the flow's subject, and each line of the ledger. The ledger's file lacks the lines of the
// book's last update while kiln has yet to append them. No step of a flow comes part way through that append.
func (s *scratch) kept() (rec map[string]any, lines []ledgerLine) {
	kiln := filepath.Join(s.work, ".git", "kiln")
	var book struct {
		Specs  []map[string]any  `json:"specs"`
		Lines  []json.RawMessage `json:"lines"`
		Ledger int               `json:"ledger"`
	}

	raw, _ := os.ReadFile(filepath.Join(kiln, "state.json"))
	_ = json.Unmarshal(raw, &book)
	for _, r := range book.Specs {
		if r["name"] == string(s.subject) {
			rec = r
		}
	}

	ledger, _ := os.ReadFile(filepath.Join(kiln, "ledger.jsonl"))
	texts := slices.Collect(strings.Lines(string(ledger)))
	for _, line := range book.Lines {
		var text bytes.Buffer
		if len(ledger) == book.Ledger && json.Compact(&text, line) == nil {
			texts = append(texts, text.String()+"\n")
		}
	}

	for _, text := range texts {
		e := ledgerLine{text: text}
		if json.Unmarshal([]byte(text), &e) == nil {
			lines = append(lines, e)
		}
	}

	return rec, lines
}

// unended reads lines for each build with no outcome. It reads them for the start line of each run with no end, by
// its session. broken lists each audit run that ended with a result, yet holds no tally.
func unended(lines []ledgerLine) (builds map[string]bool, runs map[string]ledgerLine, broken []string) {
	builds, runs = map[string]bool{}, map[string]ledgerLine{}
	for _, e := range lines {
		switch {
		case e.Kind == "build", e.Kind == "monitor" && e.Found == "fix":
			builds[e.Build] = true
		case e.Kind == "outcome":
			delete(builds, e.Build)
		case e.Kind == "start":
			runs[e.Session] = e
		case e.Kind == "end":
			if runs[e.Session].Role == "audit" && e.untallied() {
				broken = append(broken, "an audit run ended with a result, but its line holds no tally: "+e.text)
			}

			delete(runs, e.Session)
		}
	}

	return builds, runs, broken
}

// untallied is whether the end line e says its run ended with a result, yet holds no tally.
func (e ledgerLine) untallied() bool {
	return e.Ended == "result" && e.Tally == nil
}

// unanswered lists each run that started past the ledger's first calm lines, and ended with no result. No kiln was
// killed since the ledger held those lines, so each session started since ran to its end.
func (s *scratch) unanswered() []string {
	_, lines := s.kept()
	if s.calm < 0 || s.calm > len(lines) {
		return nil
	}

	var found []string
	started := map[string]bool{}
	for _, e := range lines[s.calm:] {
		started[e.Session] = started[e.Session] || e.Kind == "start"
		if e.Kind != "end" || !started[e.Session] {
			continue
		}

		if e.Ended == "no result" {
			found = append(found, "a run that no kill cut short ended with no result: "+e.text)
		}
	}

	return found
}

// stands answers the record rec as JSON, less its data. Each ID, commit, count and mark there reads "set".
func stands(rec map[string]any) string {
	stands := maps.Clone(rec)
	for _, field := range data {
		delete(stands, field)
	}

	for field, value := range stands {
		// The state and the kind of a stop are what the next kiln decides by. A seat says what kiln owes it.
		seat, isSeat := value.(map[string]any)
		switch {
		case isSeat:
			stands[field] = seatStands(seat)
		case !slices.Contains([]string{"state", "stop"}, field):
			stands[field] = "set"
		}
	}

	in, _ := json.Marshal(stands)
	return string(in)
}

// seatStands is seat, a record's seat, less its rounds. It says whether the seat holds a session and a run's mark. It
// counts the messages kiln owes it.
func seatStands(seat map[string]any) map[string]any {
	stands := map[string]any{}
	for field, value := range seat {
		owed, isOwed := value.([]any)
		switch {
		case field == "rounds":
		case isOwed:
			stands[field] = len(owed)
		default:
			stands[field] = "set"
		}
	}

	return stands
}

// shows says what the sessions and main show of the record rec. It says whether each of its sessions has printed,
// and whether main holds its landing. It says whether the turn kiln told its session has begun.
func (s *scratch) shows(rec map[string]any, refs map[string]string) []string {
	var shows []string
	for _, field := range []string{"code", "verify"} {
		seat, _ := rec[field].(map[string]any)
		id, held := seat["session"].(string)
		if !held {
			continue
		}

		shows = append(shows, fmt.Sprintf("%s begun: %v", field, s.streamed(id) > 0))
		if from, marked := seat["from"].(float64); marked {
			shows = append(shows, fmt.Sprintf("%s's run begun: %v", field, s.streamed(id) != int64(from)))
		}
	}

	if landing, held := rec["landing"].(string); held {
		shows = append(shows, fmt.Sprintf("main holds its landing: %v", refs["refs/heads/main"] == landing))
	}

	return shows
}

// streamed is the length of the stream of the session id.
func (s *scratch) streamed(id string) int64 {
	info, err := os.Stat(filepath.Join(s.work, ".git", "kiln", "sessions", id, "stream.jsonl"))
	if err != nil {
		return 0
	}

	return info.Size()
}

// amended says so when the branch of the flow's subject holds amendments to its spec. kiln accept folds them in a
// commit, and reads from the branch whether it has.
func (s *scratch) amended(refs map[string]string) []string {
	tip, held := refs["refs/heads/kiln/"+string(s.subject)]
	if _, read := s.amends[tip]; held && !read {
		path := tip + ":.kiln/specs/" + string(s.subject) + ".amend.md"
		s.amends[tip] = exec.Command("git", "-C", s.work, "cat-file", "-e", path).Run() == nil
	}

	if !s.amends[tip] {
		return nil
	}

	return []string{"its branch holds amendments"}
}

// checkouts names each directory, worktree and branch of the flow's subject that is there. A kiln killed as it makes
// or removes them leaves some, and the next kiln makes or removes the rest.
func (s *scratch) checkouts(refs map[string]string) []string {
	var there []string
	of := func(what, name string) {
		for _, role := range []string{"", ".audit-", ".gate-", ".verify-"} {
			if name == role+string(s.subject) {
				there = append(there, what+" "+role+"<name>")
			}
		}
	}

	dirs, _ := filepath.Glob(filepath.Join(envValue(s.env, "HOME"), "Library", "Caches", "kiln", "*", "*"))
	for _, dir := range dirs {
		of("the directory", filepath.Base(dir))
	}

	for _, tree := range s.worktrees() {
		of("the worktree", tree)
	}

	for _, ref := range slices.Sorted(maps.Keys(refs)) {
		if side, ok := strings.CutPrefix(ref, "refs/heads/kiln/"+string(s.subject)); ok {
			there = append(there, "the branch kiln/<name>"+side)
		}
	}

	return there
}

// moved lists the rule main breaks when it moved to a commit the record had not named. main is the commit main is
// at, and rec the subject's record. moved keeps both for the next call.
func (s *scratch) moved(rec map[string]any, main string) []string {
	was, named := s.main, s.named
	s.main = main
	s.named, _ = rec["landing"].(string)
	switch {
	case was == "", was == main, named == main:
		return nil
	}

	return []string{fmt.Sprintf("main moved from %s to %s, and the record named no such landing first", was, main)}
}

// shrunk lists the rule the ledger breaks when it lost or changed a line it held. lines is the ledger now, which
// shrunk keeps for the next call.
func (s *scratch) shrunk(lines []ledgerLine) []string {
	held := s.ledger
	s.ledger = lines
	for i, e := range held {
		if i >= len(lines) || lines[i].text != e.text {
			return []string{"the ledger lost or changed a line it held: " + e.text}
		}
	}

	return nil
}

// worktrees lists the worktrees git holds beside the main checkout, each by its directory's name, from git's files.
func (s *scratch) worktrees() []string {
	var trees []string
	links, _ := filepath.Glob(filepath.Join(s.work, ".git", "worktrees", "*", "gitdir"))
	for _, link := range links {
		raw, _ := os.ReadFile(link)
		trees = append(trees, filepath.Base(filepath.Dir(strings.TrimSpace(string(raw)))))
	}

	return trees
}

// refs reads each ref of the scratch repository, by its full name, from git's files.
func (s *scratch) refs() map[string]string {
	refs := map[string]string{}
	git := filepath.Join(s.work, ".git")
	packed, _ := os.ReadFile(filepath.Join(git, "packed-refs"))
	for line := range strings.Lines(string(packed)) {
		if hash, name, ok := strings.Cut(strings.TrimSpace(line), " "); ok && strings.HasPrefix(name, "refs/") {
			refs[name] = hash
		}
	}

	_ = filepath.WalkDir(filepath.Join(git, "refs"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			raw, _ := os.ReadFile(path)
			name, _ := filepath.Rel(git, path)
			refs[name] = strings.TrimSpace(string(raw))
		}

		return nil
	})

	return refs
}

// mainBroken lists each rule main breaks. Main holds a landing f does not land, or one twice, or the audit's file.
func (s *scratch) mainBroken(f flow, refs map[string]string) []string {
	var found []string
	landed, stray := s.onMain(refs)
	fits := len(landed) <= len(f.landed)
	for i := range min(len(landed), len(f.landed)) {
		fits = fits && regexp.MustCompile(f.landed[i]).MatchString(landed[i])
	}

	if !fits {
		found = append(found, fmt.Sprintf("main holds the landings %q, not the first of %q", landed, f.landed))
	}

	if stray {
		found = append(found, "main holds the file the audit wrote")
	}

	return found
}

// onMain reads the subject of each commit main holds past its first. It reads whether main holds the file the
// audit wrote. It asks git once for each commit main is at.
func (s *scratch) onMain(refs map[string]string) (landed []string, stray bool) {
	tip := refs["refs/heads/main"]
	if _, read := s.landed[tip]; !read {
		subjects := s.read("git", "log", "--reverse", "--format=%s", "main")
		s.landed[tip] = strings.Split(strings.TrimSpace(subjects), "\n")[1:]
		s.stray[tip] = strings.Contains(s.read("git", "ls-tree", "-r", "--name-only", "main"), "audit-stray.txt")
	}

	return s.landed[tip], s.stray[tip]
}
