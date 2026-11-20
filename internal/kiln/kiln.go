// Package kiln builds the specs a developer approved, and lands them on main.
//
//	r, err := kiln.Open(ctx, ".", os.Environ())
//	err = r.Init(ctx, os.Stdout, kiln.InitOptions{})  // .kiln/, as Go's preset, and the pre-commit hook
//	err = r.Queue(ctx, "x", os.Stdout)        // queues .kiln/specs/x.md, as main holds it
//	err = r.Fire(ctx, os.Stdout)              // builds each queued spec, one at a time
//	err = r.Status(ctx, os.Stdout)
//	err = r.TUI(ctx, "", os.Stdin, os.Stdout) // every build, live and past, in a full-screen view
//	err = r.Accept(ctx, "x", os.Stdout)       // takes the work: lands its amendments, and retires the spec
//	err = r.Reject(ctx, "x", os.Stdout)       // turns the amendments away: drops the build, and its branch
//	err = r.MonitorRun(ctx, "", os.Stdout)    // runs each monitor kiln.json names that is due
//	names, err := r.Takes(ctx, "queue")       // the specs kiln queue would not refuse
//
// Where each command moves a spec:
//
//	(no record) ─queue─▶ queued ─fire─▶ building
//	                                    ├─ lands ──▶ waiting for UAT
//	                                    │            ├─ accept ──▶ retired
//	                                    │            └─ queue, once its spec on main changed ──▶ queued
//	                                    ├─ stops ──▶ stopped
//	                                    │            └─ queue ──▶ queued
//	                                    └─ amends ─▶ waiting on its branch with amendments
//	                                                 ├─ reject ──▶ (no record)
//	                                                 └─ accept ──▶ landing accepted amendments
//	                                                               ├─ lands ──▶ retired
//	                                                               └─ stops ──▶ landing accepted amendments, stopped
//	                                                                            ├─ reject ──▶ (no record)
//	                                                                            └─ accept ──▶ landing again
//
// A spec landing accepted amendments is queued, building or stopped, as the build that lands it stands. Every other
// command refuses. A monitor's fix lives in the book under the monitor's name:
//
//	(no record) ─fix─▶ building ├─ lands, is unchanged, asks, fails, couldn't run or meets a limit ──▶ (no record)
//	                            ├─ stops ──▶ stopped, until its next fix replaces it
//	                            └─ passes, accept in kiln.json ──▶ waiting on its branch
//	                                                               ├─ reject ──▶ (no record)
//	                                                               └─ accept ──▶ landing an accepted fix
//	                                                                             ├─ lands ──▶ (no record)
//	                                                                             ├─ stops ──▶ accept or reject again
//	                                                                             └─ a conflict, or a failed gate,
//	                                                                                with a session ──▶ back to the
//	                                                                                session, as a round
//
// kiln queue refuses a fix's record, and a spec kiln.json names a monitor of.
//
// kiln queue takes only a spec whose Status, at main's tip, is Approved. Its form follows /shape's template, as kiln
// embeds it. Its ## sections all appear, in the template's order, with any of the spec's own between them. Each ###
// N. Title under Acceptance tests holds a Given, a When and a Then, numbered from 1.
//
// # Files
//
// A project tells kiln how its builds run in .kiln/, on main. kiln init starts it from a preset, go or python. A
// preset is kiln.json and the "Tailor this" sections of the build, verify and audit agents:
//
//	.kiln/kiln.json             the gate, its rounds, the hosts a session reaches, commands that print writable paths,
//	                            the MCP servers a session calls, the gauges, the workflow, where the acceptance
//	                            tests live, the harness, the paths the audit reads again, and the monitors
//	.kiln/monitors/             the scripts a monitor's check runs, out of a fix's reach
//	.kiln/gauges/               the scripts a gauge's command runs, which a monitor may name in place of a check
//	.kiln/agents/build.md       the build agent
//	.kiln/agents/verify.md      how the acceptance tests are written, and a split build's verify agent
//	.kiln/agents/audit.md       the audit
//	.kiln/specs/<name>.md       a spec, and <name>.amend.md, its amendments
//
// kiln keeps its own files out of the working tree. Main is the branch named main:
//
//	<git common dir>/kiln/         shared by every worktree of the repository
//	  state.json                   each spec's state, and the gate's last checks at main
//	  lock                         held while kiln changes state.json
//	  specs.lock                   held by the one kiln at work in the specs' lane, and names it
//	  monitors.lock                held by the one kiln at work in the monitors' lane, and names it
//	  commands.lock                held while kiln runs what judges the work or moves main, as Lanes says
//	  worktrees.lock               held while git adds or removes a worktree, which writes the shared config
//	  ledger.jsonl                 every build, session run, gate, outcome, gauge reading and monitor run
//	  monitor.log                  what the launchd job of kiln monitor install printed
//	  sessions/<id>/               each session's files, as go doc ./internal/claude lists them
//	<user cache dir>/kiln/         ~/Library/Caches/kiln/
//	  build.lock                   shared by each kiln that drives a build session; HoldBuilds takes it alone
//	  <repo>-<8 hex>/              <trees>, the repository's checkouts, named for its directory and git common dir
//	    <name>/                    a build's or a fix's worktree on kiln/<name>, or a split build's stack
//	    .audit-<name>/             the audit's worktree, at the build's commit
//	    .gate-<name>/              where the gate runs, at the build's commit
//	    .build-<name>/             a split build's clone for its build session
//	    .verify-<name>/            a split build's clone for its verify session
//	    .main/                     where the gate runs at main
//	    .gauges/                   where gauges are read after a landing
//	    .monitor-check/            where a monitor's check runs, or its gauge
//
// kiln removes every checkout a build made once it lands, is accepted, rejected or queued anew. A build that stopped,
// or waits on its branch, keeps them. Only a kiln that holds commands.lock uses .main or .gauges. Only the kiln that
// holds the monitors' lane uses .monitor-check.
//
// # Agents
//
// kiln queue reads the build, verify and audit agents' files at the commit it queues. It refuses one it can't use.
// Every session of the build runs with them as they stood there. An agent's file names the session's tools, model,
// effort and advisor in its frontmatter. Without tools, it has every tool. Without model, it runs Opus. Without effort
// or advisor, kiln names none, and each is Claude Code's own. Its body comes first in the session's system prompt.
// Then comes kiln's contract with the agent, which names where the tests live. In build then audit, the build session
// writes the tests too. Its prompt then holds the verify agent's body after the build agent's. No file changes the
// contract, the denied commands, the forbidden actions, or the guard on .kiln/. The hosts, writable paths and MCP
// servers are kiln.json's at main's tip, and none without its keys. kiln runs each command that prints paths once
// per kiln command. One that fails stops the build.
//
// kiln queue refuses a kiln.json whose acceptance names no path, a path without {spec}, or one that starts with it.
// It refuses a path that path.Clean changes, but for its last slash, or one that starts with a slash. It refuses a
// list in which one path's part before {spec} starts another's, or equals it, with case folded. It refuses a spec
// when a harness path is one of its paths, or holds one, or lies under one. It refuses a build agent that says how
// to write the tests. Such a line names a path's part before {spec}, where the part starts a path.
//
// # Builds
//
// One build, as kiln fire runs it. Each "back" sends a report to the same build session, which then works on:
//
//	checkout   kiln/<name>, from main's tip, in <trees>/<name>
//	session    the build session works, and ends its turn
//	commit     kiln commits whatever the session left; a merge it left in conflict: back, as a round
//	gate       fails: back, as a round; past kiln.json's rounds, the build stops
//	audit      a session of its own, in <trees>/.audit-<name>, up to 3 tries; findings: back, with no round spent
//	           none when the build changed nothing outside .kiln/
//	           again, as a round, once the work changes a path kiln.json's reaudit names since the audit read it
//	amended?   .kiln/specs/<name>.amend.md: the build waits on its branch, for kiln accept or kiln reject
//	rebase     onto main's tip, as one commit; a conflict, or nothing changed on main: back, as a round
//	gate       again, on that commit; fails: back, as a round
//	move       main to that commit, if main is still where the rebase found it
//	           main moved: rebase and gate again, with no round spent
//	           main's checkout busy, or the developer's files in the way: the build stops
//
// Each gate waits for commands.lock, as Lanes says. A landing's try holds it from reading main's tip until main
// moves, or the work goes back. Its gate takes none of its own.
//
// The audit reads each directory holding a file the build changed outside .kiln/. For a directory it emptied, it reads
// the nearest above. Nothing the audit writes reaches the build, and kiln removes its worktree once it ends. A
// landing that doesn't move main leaves the branch as it stood before the landing began. Should kiln die once main
// holds the landing, the next kiln fire records it landed, and starts no session. kiln accept lands a commit that
// retires the spec. A spec that waited on its branch lands its work first, with the amendments folded in.
//
// A usage limit pauses the build until the limit resets, and then resumes the same session. A limit with no reset
// pauses it for 5 minutes.
//
// A split build, which kiln.json's "workflow": "split" names at the commit queued, runs two sessions at once:
//
//	build session    in <trees>/.build-<name>, a clone of main on kiln/<name>.build: writes the code
//	verify session   in <trees>/.verify-<name>, a clone of main on kiln/<name>.verify: writes the acceptance tests,
//	                 from the spec alone, and alone amends the spec
//
//	gate the build side   fails: to the build session, as a round
//	stack                 kiln/<name> at the build side, the verify side merged in; a conflict: to verify, as a round
//	gate the stack        fails: to verify, as a round; each bug its "bugs:" line names goes to build
//	audit the stack       findings: to both
//	land                  as a build does; each setback goes to the side that owns it
//
// A build reply whose last line starts "cannot hold:" goes to the verify session. Each session's settings deny it
// reads of the other's clone and transcripts, and of kiln's other checkouts. Its hook keeps it to its side.
//
// # The gate
//
// The gate runs kiln.json's commands, read from main's tip, with sh -c at the build's commit and at main. It passes
// when every command exits 0, but for tests that fail at main too. A command names its failed tests in go test's
// output. Or it writes them as JUnit XML, to the path kiln gives it in $KILN_JUNIT. A failure that names no test is
// never excused. With JUnit XML, a run cut short fails too: each test run at main must run at the build's commit.
// kiln keeps the last 16 KiB of each command's output. It runs them at main again only once
// main or the commands change. The gate holds commands.lock, with the check or gauge it reads, so no gate runs beside
// another. Its line in the ledger starts once it holds the lock, so a build's time counts no wait.
//
// The gate also fails on a change to a guarded path, however the path was written. A session's hook refuses an Edit
// or a Write there before it happens. Each name is folded as APFS folds it:
//
//	a .git directory          only git and kiln write there
//	.kiln/                    but .kiln/specs/<name>.amend.md, which a split build's verify session alone writes;
//	                          a monitor's fix writes nothing there
//	a .claude directory       the developer's Claude Code settings, agents and skills, whose hooks run unsandboxed
//	the other side            in a split build, the build session writes neither the acceptance tests nor the
//	                          amendments; the verify session writes only those, and the harness
//	the acceptance tests      to a monitor's fix, each path starting with an acceptance path's part before {spec},
//	                          unless its monitor says "tests": true
//
// # Monitors
//
// A monitor is a job kiln.json names, which kiln monitor run runs at main's tip once its interval has passed:
//
//	{"name": "deps", "every": "24h", "check": ".kiln/monitors/deps", "command": "go get -u ./... && go mod tidy",
//	 "agent": "deps.md", "prompt": "Make the gate pass."}
//
// kiln.json refuses a monitor with no name, a name another has, or one no spec could have. It refuses an interval under
// an hour, or not in whole hours. It refuses a monitor with no command and no agent. It refuses an agent that is no
// file directly under .kiln/agents/, or that is the audit. It refuses a monitor that may not write the tests while kiln
// cannot guard them. acceptance then names no path, or one with no part before {spec}. It refuses a gauge that gauges
// lacks. It refuses a gauge beside a check, or beside gates. It refuses drain with neither a check nor a gauge.
//
// One kiln monitor run:
//
//	lock       monitors.lock, the monitors' lane, beside any kiln fire; build.lock shared
//	recover    the monitors' lane alone, as Lanes says: drop each fix a kiln cut short, or end it landed when main
//	           holds its landing; then land each fix's accepted landing that is queued, but no spec's
//	each monitor kiln.json names, in its order, or the one named:
//	  due?     skipped unless due, or named
//	  held?    held while a fix of it builds, waits or lands; held by its last fix, as below, unless named
//	  check    sh -c in <trees>/.monitor-check at main's tip, or its gauge there; no finding: healthy; it holds
//	           commands.lock from before it reads main's tip, so it runs at the main its fix starts from
//	  fix      a build with no spec on kiln/<name>, from main's tip
//	           command   first, outside the sandbox, then the gate; fails with no agent: stopped
//	           agent     while the gate fails, or with no command; kiln.json's rounds, and the audit
//	  ends     landed, waits on its branch, unchanged, needs you, session failed, usage limit, stopped, couldn't run
//	  tab      a Ghostty tab, when the fix landed, waits, stopped, needs you or session failed
//	  gauges   read after a landing, as after a build's
//	  again    with "drain": true, after a fix that landed: from check, due or not, until no fix lands
//
// A monitor is due when it never ran, or its check or gauge couldn't run. It is due when a usage limit ended its fix,
// or its fix was cut short. Otherwise it is due once its last run started its interval, less 30 minutes, ago.
// $KILN_NOW, as RFC 3339, stands for the clock. A monitor with neither check nor gauge starts a fix each run it is due.
//
// Its check's last line of stdout is {"fix": true|false, "summary": "..."}. A check that exits non-zero, or prints
// no such line, couldn't run. kiln then keeps its last line of stdout and stderr. The agent's file names its role.
// Its session reads what the check printed above its last line, what the gate printed, then the prompt. Without a
// prompt, it reads "Fix what the monitor <name> found, and end your turn." The gate adds the check, unless gates is
// false, and fails while it asks for a fix. A fix that passes lands, or waits for kiln accept when accept is true.
// In an accepted fix's landing, a conflict with main or a failed gate goes back to the fix's session. Each spends a
// round. Before a failed gate goes back, kiln merges main into the branch. A fix with no session stops instead.
// Work that changes nothing ends unchanged. An agent's reply whose last line is {"ask": true, "summary": "..."} ends
// the fix needs you, with its report. One whose last line is {"title": "type(scope): summary"}, of at most 72
// characters, titles the commit that lands the fix. A landing keeps the title of the fix the developer accepted. A
// failed session ends it session failed. A usage limit ends it at once, never waiting for the reset. In a landing,
// each of these stops it and keeps the fix. So does work that changes nothing on main. Only a fix whose agent ran
// gets the audit. A landing's audit reads each file whose own change since main differs from the accepted commit's.
// It runs when the work so differs from the commit the audit last read, outside .kiln/. It skips the session's
// answer to its findings, but for a path kiln.json's reaudit names. A landing whose turn kiln cut short resumes
// that turn first.
//
// A monitor may name a gauge in place of a check, as "gauge": "health". Its run reads the gauge over the whole
// repository. It reads in the check's worktree at main's tip, with git's empty tree in $KILN_BASE. A reading that
// crossed its line starts a fix, and one on its line is healthy. A failed command, or no number, couldn't run. The
// reading is the monitor's summary, and its ledger line holds it. The agent reads what the gauge printed above its
// number. At each gate of the fix, kiln reads the gauge again, by kiln.json at main's tip. It reads in the check's
// worktree at the fix's commit, never the fix's own worktree. $KILN_BASE is then the fix's merge base with main. The
// gate fails while that reading crosses the line, or the command fails. Either goes back to the agent. A reading of
// no number never crossed, so a fix that deletes what crossed lands.
//
// A monitor is held, and starts no fix, by its last fix's outcome. A change since that fix started releases it:
//
//	last fix                                held until a change to
//	stopped, or rejected                    main's tip, the monitor's entry in kiln.json, or what it printed
//	needs you, with a check or a gauge      its entry, or what it printed
//	needs you, with neither                 main's tip, or its entry
//	unchanged, with a check or a gauge      its entry, or what it printed
//	landed, with a gauge                    its entry, or what its gauge printed
//	landed, with a check out of the gate    what its check printed
//	any other                               not held
//
// kiln monitor run with a name skips that hold, but never starts a second fix.
//
// kiln asks Ghostty, through osascript, for a tab in its front window, or a window when it shows none. The app and the
// tab in front stay in front. The tab runs <kiln> monitor show <name> in <top>. $KILN_OPEN names a command to run in
// place of osascript, with that command line and <top> as its arguments. The tab notifies the developer, and shows the
// summary, report or error, and the next command. A session that failed as the last run did opens none. A landing
// kiln accept makes opens none, and one that kiln monitor run takes up as it starts opens one.
//
// # Lanes
//
// A lane is the work that one kiln at a time may do in a repository. The specs' lane builds specs, and the monitors'
// lane fixes for monitors. A record in state.json belongs to one lane: a fix's to the monitors', and a spec's to the
// specs'. kiln fire works in the specs' lane, and kiln monitor run in the monitors', at the same time:
//
//	kiln monitor run ── fix ── fix ── fix ──▶      the monitors' lane
//	kiln fire ───────── build x ── build y ──▶     the specs' lane
//
// kiln accept takes the lane of the record it lands, and holds it through the whole landing. A kiln accept that
// retires a spec waiting for UAT takes none. A second kiln in a lane refuses at once, and names the kiln that holds
// it. A kiln accept that finds its lane held leaves its landing queued. The kiln at work in the specs' lane lands a
// spec's, or the next kiln fire should it stop first. The next kiln monitor run lands a fix's.
//
// As it takes its lane, a kiln recovers what a dead kiln left there. It leaves the other lane's records as they stand,
// since a live kiln may work on them. The lane's lock decides, and no process ID:
//
//	                          the specs' lane                    the monitors' lane
//	lost runs, in the ledger  ends each lost run of a build      ends each lost run of a fix
//	records left building     queues each spec's again           queues each fix's accepted landing again
//	a fix left building       never                              kiln monitor run drops each not yet accepted
//
// The sessions of both lanes run side by side. Across both, kiln runs one at a time what judges the work, or moves
// main. That is each gate, with the check or gauge it reads, and each try of a landing. It is a monitor's check or
// gauge at main's tip, and the read of the gauges after a landing. Each holds commands.lock. A kiln that finds it held
// prints one line. It tries the lock again each 200 ms, until it holds it or kiln is interrupted:
//
//	<name>: waits while another kiln runs the project's commands
//
// A session, an audit, a monitor's command and a pause for a usage limit hold no lock. kiln precommit takes none. A
// kiln takes its lane first, then commands.lock, and never takes a lane while it holds commands.lock.
//
// # Hangs
//
// No wait of kiln's lasts forever, so a hang never holds a lane, commands.lock or the launchd job. kiln.json's
// timeout, a Go duration read at main's tip, bounds two waits. It is 1 hour when unset:
//
//	a command kiln runs for the project    the gate's, a monitor's check or command, a gauge, a writable command
//	a session                              how long it prints nothing
//
// kiln kills what runs past the timeout, with each process of its group. Its words name what ran past, the timeout
// and the setting:
//
//	what ran past                                          ends in
//	a gate's command, a writable command, a monitor's      a stopped build or fix, with those words as its why
//	command, a gauge in a fix's gate, or a session
//	a monitor's check, or its gauge at main's tip          couldn't run, with those words as its summary
//	a gauge read after a landing                           unknown, with those words as why
//
// The timeout counts only the time the Mac is awake, as Go's timers do. It never sends work back as a round. A round
// would wait out the same hang again, holding the same lock. A pause for a usage limit lasts until the limit resets.
// A wait for commands.lock lasts as long as the bounded work that holds it.
//
// # The ledger
//
// Each queue is a new build, with its own ID. kiln's ledger records every build, session run and gate, and how each
// build ended. A build's time is its session runs' time plus its gates'. A pause for a usage limit adds nothing. Its
// turns, cost and failed calls sum its runs. A number kiln could not read shows as unknown, never as zero. A landed
// build's commit holds its time, cost, turns and failed calls. An accepted fix's landing commit adds a line before
// them, which names each file changed since kiln accept:
//
//	Changed since kiln accept: <path>, <path>
//
// # Gauges
//
// A gauge is a command kiln.json names, which prints a number, and a line: min or max, never both.
//
//	{"name": "mutation", "command": ".kiln/gauges/mutation-score \"$KILN_BASE\"", "min": 80}
//
// Once kiln lands a build or a fix on main, kiln reads each gauge kiln.json names at main's tip. It runs each command
// in turn with sh -c, in <trees>/.gauges at the landed commit. $KILN_BASE holds main's tip before the landing, so a
// command can measure one build's work alone. In a monitor's run, $KILN_BASE is git's empty tree. There git diff
// "$KILN_BASE" HEAD names every file, and git log "$KILN_BASE"..HEAD fails. The worktree holds only the commit's files.
// So a command that reads the changed files should list them with git diff --diff-filter=d. The reading is the last
// line of stdout, trimmed, as a decimal number. A command that fails, or prints no number last, reads unknown. kiln
// says why, by the last line of its stderr, or of its stdout. kiln writes each reading to its ledger with its line, and
// judges it against that line wherever it shows. A reading below min, or above max, crossed its line. An unknown one
// never did. The kiln that landed the build prints the readings after its summary, and only then starts the next build.
// A gauge never blocks a landing, and nothing reads a landing kiln missed. The read holds commands.lock, so it waits
// for a gate, and a gate waits for it.
//
// # Notifications
//
// A build that ends waiting on the developer notifies them, in the output of the kiln that built it. It ends stopped,
// waiting for UAT, or waiting on its branch. kiln prints an OSC 9 sequence, which a terminal such as Ghostty shows as
// a notification. Its body is "kiln: <name> <state>", with the state as kiln status words it. kiln prints it whether
// or not the output is a terminal. A summary of the build follows, from kiln's record, its ledger and git:
//
//	x: stopped: the gate still failed after its last round:
//	  12 min · $3.40 · 41 turns · 2 failed calls
//	  3 of 3 rounds · the audit had not run
//	  failing: TestAT2 (example.com/scratch)
//	  the work is on kiln/x, in ~/Library/Caches/kiln/scratch-1f2e3d4c/x
//	  next: cd ~/Library/Caches/kiln/scratch-1f2e3d4c/x && claude --resume <session>, to ask the session why;
//	        then fix main or the spec, and run kiln queue x
//
// A landed build shows the files its commit changed, and a build on its branch those it changed since main. That
// build shows its amendments too. The last line is the next command, never one the spec's state refuses.
package kiln

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

const (
	mainBranch = "main"
	mainTree   = ".main"
	product    = "PRODUCT.md"
	// kilnDir holds what a project tells kiln: its settings, its agents and its specs.
	kilnDir = ".kiln"
)

// noMain says a repository has no branch main, or no commit on it, and how to make one. kiln init prints it.
const noMain = "kiln builds on the branch " + mainBranch + ", which this repository lacks: commit to " + mainBranch +
	", or run git branch -m " + mainBranch

// errNoMain refuses a command that reads main, in a repository that noMain describes.
var errNoMain = fmt.Errorf("%w: %s", errRefused, noMain)

// mainTip is the commit main points to. It answers errNoMain while the repository has no such branch.
func (r *Repo) mainTip(ctx context.Context) (git.Rev, error) {
	tip, err := r.git.Tip(ctx, mainBranch)
	if err == nil && tip == "" {
		err = errNoMain
	}

	return tip, err
}

// Repo is one repository kiln builds, opened from any of its checkouts.
type Repo struct {
	top    string
	common string
	git    *git.Git
	store  store
	trees  string
	env    []string
	exe    string
	claude claude.Claude

	// writable holds the paths each command printed, by the command, once it ran. It is read and written only while
	// holding writableMu.
	writableMu sync.Mutex
	writable   map[string][]string
}

// Open opens the repository that holds dir. env is the environment of every process kiln starts, less what each
// withholds.
func Open(ctx context.Context, dir string, env []string) (*Repo, error) {
	top, err := plainGit(ctx, dir, proc.Git.Env(env), "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is in no git repository: %w", dir, err)
	}

	g, err := git.New(top, env)
	if err != nil {
		return nil, err
	}

	if err := g.Check(ctx); err != nil {
		return nil, err
	}

	common, err := g.CommonDir(ctx)
	if err != nil {
		return nil, err
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}

	cache, err := cacheDir()
	if err != nil {
		return nil, err
	}

	kept := filepath.Join(common, "kiln")
	sum := sha256.Sum256([]byte(common))
	return &Repo{
		top:    top,
		common: common,
		git:    g,
		store:  store{dir: kept},
		trees:  filepath.Join(cache, fmt.Sprintf("%s-%x", filepath.Base(top), sum[:4])),
		env:    env,
		exe:    exe,
		claude: claude.New(claude.Config{Program: "claude", Env: env, Sessions: filepath.Join(kept, "sessions")}),
	}, nil
}

// cacheDir is kiln's directory under the user's cache directory.
func cacheDir() (string, error) {
	cache, err := os.UserCacheDir()
	return filepath.Join(cache, "kiln"), err
}

// HoldBuilds keeps every kiln on the machine from starting to drive a build session, until release. It refuses while
// one drives one: a kiln fire, a kiln monitor run, or a kiln accept that lands. A build's session runs kiln hook by
// its path, so the reinstall holds it while it swaps the binary.
func HoldBuilds() (release func(), err error) {
	release, err = lockBuilds(syscall.LOCK_EX | syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, fmt.Errorf("%w: a kiln drives a build session, in this repository or another; reinstall once "+
			"it ends", errRefused)
	}

	return release, err
}

// shareBuilds takes build.lock shared, as every kiln that drives a build session does. The reinstall then swaps no
// binary under one. It refuses while the reinstall holds it.
func shareBuilds() (release func(), err error) {
	release, err = lockBuilds(syscall.LOCK_SH | syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, fmt.Errorf("%w: kiln is being reinstalled; run it again once the reinstall ends", errRefused)
	}

	return release, err
}

// lockBuilds takes build.lock, in kiln's directory under the user's cache directory, as how says.
func lockBuilds(how int) (release func(), err error) {
	dir, err := cacheDir()
	if err != nil {
		return nil, err
	}

	return store{dir: dir}.lock(buildLock, how)
}

// hookMark marks a pre-commit hook as kiln's own, which kiln init may write over.
const hookMark = "# kiln's pre-commit hook"

// installHook installs the pre-commit hook in the repository's own hooks directory. Where core.hooksPath is set, or
// another pre-commit hook is there, it writes none. It then says why, and the line to add to the developer's own hook.
func (r *Repo) installHook(ctx context.Context, out io.Writer) error {
	env := proc.Git.Env(r.env)
	set, err := plainGit(ctx, r.top, env, "config", "--default", "", "--get", "core.hooksPath")
	if err != nil {
		return err
	}

	hooks, err := plainGit(ctx, r.top, env, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	if err != nil {
		return err
	}

	if set != "" {
		whose := "which other repositories may share"
		if strings.HasPrefix(hooks, r.top+string(filepath.Separator)) {
			whose = "this repository's own hooks directory"
		}

		fmt.Fprintf(out, "installed no pre-commit hook, since core.hooksPath names %s, %s; add this line to your "+
			"own pre-commit hook: kiln precommit\n", set, whose)
		return nil
	}

	hook := filepath.Join(hooks, "pre-commit")
	if old, err := os.ReadFile(hook); err == nil && !bytes.Contains(old, []byte(hookMark)) {
		fmt.Fprintf(out, "installed no pre-commit hook, since %s is another one; add this line to it: kiln precommit\n",
			hook)
		return nil
	}

	if err := os.MkdirAll(hooks, 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(hook, []byte(hookScript(r.exe)), 0o755); err != nil {
		return err
	}

	fmt.Fprintf(out, "installed %s\n", hook)
	return nil
}

// hookScript is the pre-commit hook that runs exe's precommit.
// Once exe moves or is deleted, the hook runs the kiln on the PATH instead.
func hookScript(exe string) string {
	return fmt.Sprintf(`#!/bin/sh
%s
kiln=%s
if [ ! -x "$kiln" ]; then
	if ! command -v kiln >/dev/null 2>&1; then
		echo "kiln is gone from $kiln; kiln init writes this hook again" >&2
		exit 1
	fi
	kiln=kiln
fi
exec "$kiln" precommit
`, hookMark, shellQuote(exe))
}

// Spec is a spec's name, such as checkout-flow: .kiln/specs/checkout-flow.md holds the spec.
type Spec string

const specsDir = kilnDir + "/specs"

func (s Spec) file() string      { return specsDir + "/" + string(s) + ".md" }
func (s Spec) amendPath() string { return specsDir + "/" + string(s) + amendSuffix }

// oldFile is where a commit from before .kiln/ held the spec.
func (s Spec) oldFile() string { return "specs/" + string(s) + ".md" }

// branch is the branch the spec's build works on.
func (s Spec) branch() git.Branch { return git.Branch("kiln/" + s) }

var (
	specName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	approved = regexp.MustCompile(`(?m)^Status: Approved\b`)
)

// Queue records main's tip as the commit of .kiln/specs/<name>.md, and queues a new build of it. It refuses a name
// that is not a spec's, and a spec main does not hold. It refuses one whose Status is not Approved, or whose form
// leaves the template. It refuses while main holds no kiln.json, build, verify or audit agent it can use. It refuses by
// the state the spec is in, as the package doc shows. A stopped build, or one waiting for UAT, is dropped for the new
// one.
func (r *Repo) Queue(ctx context.Context, name Spec, out io.Writer) error {
	main, err := r.mainTip(ctx)
	if err != nil {
		return err
	}

	spec, err := r.queueable(ctx, main, name)
	if err != nil {
		return err
	}

	var drop bool
	var dropped string
	err = r.store.update(func(b *book) error {
		drop, dropped, err = b.queue(name, main, r.changed(ctx, name, spec))
		return err
	})
	if err != nil {
		return err
	}

	if dropped != "" {
		r.ended(name, dropped, outDropped, out)
	}

	if drop {
		if err := r.dropCheckouts(ctx, name); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "%s: %s, as main holds it at %s\n", name, queued, short(main))
	return nil
}

// queueable reads .kiln/specs/<name>.md at main, a spec kiln queue may take. Its Status is Approved, and its form
// follows the template. Main must hold a kiln.json, a build agent and an audit agent that kiln can use.
func (r *Repo) queueable(ctx context.Context, main git.Rev, name Spec) ([]byte, error) {
	if err := r.ready(ctx, main); err != nil {
		return nil, err
	}

	spec, err := r.approvedSpec(ctx, main, name)
	if err != nil {
		return nil, err
	}

	if c, err := r.config(ctx, main); err == nil {
		if _, ok := c.monitorNamed(name); ok {
			return nil, fmt.Errorf("%w: %s names a monitor %s, and one name is never both a spec's and a "+
				"monitor's: rename one", errRefused, configFile, name)
		}

		if why := c.harnessRefusal(name); why != "" {
			return nil, fmt.Errorf("%w: %s", errRefused, why)
		}
	}

	if err := formOf(string(spec)); err != nil {
		return nil, fmt.Errorf("%w: %s leaves /shape's template: %w", errRefused, name.file(), err)
	}

	return spec, nil
}

// ready refuses while main, at main, holds no kiln.json, build agent, verify agent or audit agent that kiln can use.
// kiln.json must name where the acceptance tests live. The build agent must not say how to write them.
func (r *Repo) ready(ctx context.Context, main git.Rev) error {
	c, err := r.config(ctx, main)
	if err != nil {
		return fmt.Errorf("%w: %w", errRefused, err)
	}

	if why := c.acceptanceRefusal(); why != "" {
		return fmt.Errorf("%w: %s %s", errRefused, configFile, why)
	}

	agents := map[string]agent{}
	for _, role := range []string{buildRole, verifyRole, auditRole} {
		if agents[role], err = r.agent(ctx, main, role, initHint); err != nil {
			return err
		}
	}

	if line := testsText(agents[buildRole].body, c.fixedParts()); line != "" {
		return fmt.Errorf("%w: %s says how to write the acceptance tests, in %q: move that text to %s, which "+
			"every build reads", errRefused, agentFile(buildRole), line, agentFile(verifyRole))
	}

	return nil
}

// testsText is the first line of body that says how to write the acceptance tests, or "". An older default build
// agent said so. Such a line is its heading, or its line on what the build writes. Or it names one of fixed, where
// the tests live.
func testsText(body string, fixed []string) string {
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(line)
		names := func(part string) bool { return startsPath(line, part) }
		switch {
		case line == "## Acceptance tests",
			strings.HasPrefix(line, "- The code of each of the spec's acceptance tests"),
			slices.ContainsFunc(fixed, names):
			return line
		}
	}

	return ""
}

// startsPath is whether line holds part where part starts a path. That is at the line's start, or after a rune no
// path holds, such as a space or a backtick. So "internal/git/" does not start "git/".
func startsPath(line, part string) bool {
	for at := 0; ; at++ {
		i := strings.Index(line[at:], part)
		if i < 0 {
			return false
		}

		at += i
		before, _ := utf8.DecodeLastRuneInString(line[:at])
		if at == 0 || !inPath(before) {
			return true
		}
	}
}

// inPath is whether r is a rune a path's name commonly holds.
func inPath(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-/", r)
}

// approvedSpec reads .kiln/specs/<name>.md at main, a spec whose Status is Approved.
func (r *Repo) approvedSpec(ctx context.Context, main git.Rev, name Spec) ([]byte, error) {
	if !specName.MatchString(string(name)) {
		return nil, fmt.Errorf("%w: %q is not a spec's name, such as checkout-flow", errRefused, name)
	}

	spec, found, err := r.git.Show(ctx, main, name.file())
	switch {
	case err != nil:
		return nil, err
	case !found:
		return nil, fmt.Errorf("%w: main holds no %s: commit it there first", errRefused, name.file())
	case !approved.Match(spec):
		return nil, fmt.Errorf("%w: %s is not Approved: finish it with /shape, and commit it, first", errRefused,
			name.file())
	}

	return spec, nil
}

// changed answers whether the spec of name at a commit differs from spec.
func (r *Repo) changed(ctx context.Context, name Spec, spec []byte) func(since git.Rev) (bool, error) {
	return func(since git.Rev) (bool, error) {
		old, _, err := r.specAt(ctx, since, name)
		return !bytes.Equal(old, spec), err
	}
}

// specAt reads the spec of name as the commit at held it. That is .kiln/specs/<name>.md, or specs/<name>.md where
// at holds no spec under .kiln/. A commit from before .kiln/ held its specs there.
func (r *Repo) specAt(ctx context.Context, at git.Rev, name Spec) (spec []byte, found bool, err error) {
	spec, found, err = r.git.Show(ctx, at, name.file())
	if err != nil || found {
		return spec, found, err
	}

	return r.git.Show(ctx, at, name.oldFile())
}

// ended writes to the ledger that the build of name ended with outcome.
func (r *Repo) ended(name Spec, build, outcome string, out io.Writer) {
	if err := r.store.write(entry{Kind: outcomeKind, Build: build, Outcome: outcome}); err != nil {
		fmt.Fprintf(out, "%s: kiln could not write its ledger: %v\n", name, err)
	}
}

// Reject turns away the amendments of a spec waiting on its branch, or of its stopped landing. It drops the build, its
// worktree and its branch, and kiln holds no record of the spec. Main is as it was.
func (r *Repo) Reject(ctx context.Context, name Spec, out io.Writer) error {
	var rec record
	err := r.store.update(func(b *book) (err error) {
		rec, err = b.reject(name)
		return err
	})
	if err != nil {
		return err
	}

	if rec.Monitor {
		r.ended(name, rec.Build, outRejected, out)
	} else {
		r.ended(name, rec.Build, outDropped, out)
	}

	if err := r.dropCheckouts(ctx, name); err != nil {
		return err
	}

	fmt.Fprintf(out, "%s: rejected, and %s is gone\n", name, name.branch())
	return nil
}

// Takes answers the names command would not refuse, for the shell to complete. For queue, accept and reject, they
// are specs, and for accept and reject, waiting fixes too. For tui, they are the specs kiln holds or its ledger holds.
// For "monitor run" and "monitor show", they are the monitors kiln.json names. Other commands take none.
func (r *Repo) Takes(ctx context.Context, command string) ([]string, error) {
	switch command {
	case "tui":
		return r.viewed(ctx)
	case acceptCommand, rejectCommand:
		return r.taking(command)
	case queueCommand:
		return r.queueNames(ctx)
	case "monitor run", "monitor show":
		return r.monitorNames(ctx)
	}

	return nil, nil
}

// viewed answers the specs kiln tui opens on.
func (r *Repo) viewed(ctx context.Context) ([]string, error) {
	specs, err := view{r}.Specs(ctx)
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.Name
	}

	return names, err
}

// taking answers the specs in the book that command would not refuse.
func (r *Repo) taking(command string) ([]string, error) {
	b, err := r.store.read()
	var names []string
	for _, s := range b.Specs {
		if b.may(command, s.Name, nil) == nil {
			names = append(names, string(s.Name))
		}
	}

	return names, err
}

// monitorNames answers the monitors kiln.json names at main's tip.
func (r *Repo) monitorNames(ctx context.Context) ([]string, error) {
	main, err := r.mainTip(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.config(ctx, main)
	names := make([]string, len(c.Monitors))
	for i, m := range c.Monitors {
		names[i] = string(m.Name)
	}

	return names, err
}

// queueNames answers the specs on main that kiln queue would not refuse.
func (r *Repo) queueNames(ctx context.Context) ([]string, error) {
	b, err := r.store.read()
	if err != nil {
		return nil, err
	}

	main, err := r.mainTip(ctx)
	if err != nil {
		return nil, err
	}

	files, err := r.git.Files(ctx, main, specsDir)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, f := range files {
		name, ok := strings.CutSuffix(f, ".md")
		err := r.mayQueue(ctx, b, main, Spec(name))
		switch {
		case !ok, errors.Is(err, errRefused):
			continue
		case err != nil:
			return nil, err
		}

		names = append(names, name)
	}

	return names, nil
}

// mayQueue answers nil when kiln queue would take name, with main's tip at main, or why it refuses.
func (r *Repo) mayQueue(ctx context.Context, b book, main git.Rev, name Spec) error {
	spec, err := r.queueable(ctx, main, name)
	if err != nil {
		return err
	}

	return b.may(queueCommand, name, r.changed(ctx, name, spec))
}

// Status shows each spec kiln holds, and where it stands, but no monitor's fix. Then it shows each gauge kiln.json
// names at main's tip, with its latest reading. Last, it shows each monitor kiln.json names, and where it stands.
func (r *Repo) Status(ctx context.Context, out io.Writer) error {
	if _, err := r.mainTip(ctx); err != nil {
		return err
	}

	b, err := r.store.read()
	if err != nil {
		return err
	}

	specs := slices.DeleteFunc(slices.Clone(b.Specs), func(s *record) bool { return s.Monitor })
	if len(specs) == 0 {
		fmt.Fprintln(out, "kiln holds no approved spec")
	}

	for _, s := range specs {
		s.status(out)
	}

	if err := r.gaugeStatus(ctx, out); err != nil {
		return err
	}

	return r.monitorStatus(ctx, out)
}

// status shows where the spec of r stands, as kiln status does.
func (r *record) status(out io.Writer) {
	fmt.Fprintf(out, "%s: %s", r.Name, shownState(r))
	switch {
	case r.halted():
		fmt.Fprintf(out, "\n  %s", haltedNext)
	case r.Accepted && r.State == queued:
		// kiln status takes no lane's lock to learn its holder. A kiln fire started meanwhile would find it held.
		fmt.Fprintf(out, "\n  %s", queuedLands)
	case r.State == building && !r.Paused.IsZero():
		fmt.Fprintf(out, ", paused for a usage limit until %s", r.Paused.Local().Format("15:04:05 on Jan 2"))
	case r.State == amended:
		fmt.Fprintf(out, ", %s", r.Name.branch())
	}

	fmt.Fprintln(out)
	if len(r.FailingAtMain) > 0 {
		fmt.Fprintf(out, "  failing at main before the build: %s\n", strings.Join(r.FailingAtMain, ", "))
	}

	for _, text := range []string{r.Why, r.Amendments} {
		if text != "" {
			fmt.Fprintln(out, indent(text))
		}
	}
}

// queuedLands is what kiln status says of a spec's queued landing, whichever kiln holds the specs' lane.
const queuedLands = "the kiln at work on specs lands it, or the next kiln fire"

// Accept takes the developer's work on a spec. The spec is deleted from main, and PRODUCT.md gains a line that names
// the commit that last held it. The commit that moves main is gated first, on main as it stands. For a spec waiting
// for UAT, Accept takes no lane.
//
// A spec waiting on its branch first folds its amendments into the spec. It lands as a build does, in the specs'
// lane. A conflict or a failing gate goes back to its build's session. While a kiln fire or another kiln accept holds
// that lane, that kiln lands it. Should it stop first, the next kiln fire does.
//
// A monitor's fix waiting on its branch is rebased onto main, and gated again, check included. Then main moves onto
// it if main hasn't moved. It retires nothing. It lands in the monitors' lane, which Accept holds through the whole
// landing. While another kiln holds that lane, the next kiln monitor run lands it. A conflict or a failing gate goes
// back to the fix's session, as a round, and Accept runs that session. A landing that stops waits for kiln accept
// again, or kiln reject. One with no session that conflicts with main waits for kiln reject. A stopped landing lands
// again afresh, its rounds at 0, with no second fold. An accepted landing that stops fails Accept.
func (r *Repo) Accept(ctx context.Context, name Spec, out io.Writer) error {
	// Accept takes build.lock before it changes anything, so a reinstall that refuses it leaves the spec as it was.
	unshare, err := shareBuilds()
	if err != nil {
		return err
	}

	defer unshare()
	var rec record
	err = r.store.update(func(b *book) (err error) {
		rec, err = b.accept(name)
		return err
	})
	if err != nil {
		return err
	}

	b := &build{r: r, rec: rec, tree: r.tree(name), out: out}
	switch {
	case rec.Accepted:
		return r.landAccepted(ctx, b)
	case rec.Monitor:
		return r.acceptFix(ctx, b)
	case rec.State == amended:
		return r.acceptAmended(ctx, b)
	}

	return r.acceptLanded(ctx, b)
}

// acceptLanded retires the spec of b, whose build landed, in a commit on main as it stands.
func (r *Repo) acceptLanded(ctx context.Context, b *build) error {
	name := b.rec.Name
	if err := b.checkoutMain(ctx); err != nil {
		return err
	}

	landed, back, err := b.land(ctx, "", b.retire, name.file(), name.amendPath())
	if err == nil && back.report != "" {
		err = fmt.Errorf("%w: the gate failed on main as it stands, so %s is not accepted:\n%s", errRefused, name,
			back.report)
	}

	// Before the landing, the spec had no branch: its build's was gone once it landed. The landing ran the gate
	// again, in a worktree of its own.
	if err != nil {
		return errors.Join(err, r.dropCheckouts(context.WithoutCancel(ctx), name))
	}

	if err := r.dropCheckouts(ctx, name); err != nil {
		return err
	}

	if err := r.store.update(func(b *book) error { b.retire(name); return nil }); err != nil {
		return err
	}

	b.record(entry{Kind: outcomeKind, Outcome: outAccepted})
	fmt.Fprintf(b.out, "%s: accepted, and main is at %s\n", name, short(landed))
	return nil
}

// acceptAmended folds the amendments of b's spec into the spec, on its branch, and lands it. A split build folds them
// in the verify session's clone, whose side owns the amendment file. Every later stack then carries the fold.
func (r *Repo) acceptAmended(ctx context.Context, b *build) error {
	name := b.rec.Name
	at := b.tree
	if b.rec.Verify != "" {
		at = r.clone(name, verifyRole)
	}

	tip, err := r.git.Head(ctx, at)
	if err != nil {
		return err
	}

	if _, err := b.foldAmendments(ctx, at); err != nil {
		return errors.Join(err, r.git.Reset(ctx, at, tip))
	}

	err = r.store.set(name, func(s *record) { s.State, s.Accepted, s.Rounds, s.Amendments = queued, true, 0, "" })
	if err != nil {
		return err
	}

	return r.landAccepted(ctx, b)
}

// acceptFix queues the landing of b's fix, which waits on its branch, and lands it. The commit the developer
// accepted is the commit the audit last read.
func (r *Repo) acceptFix(ctx context.Context, b *build) error {
	tip, err := r.git.Head(ctx, b.tree)
	if err != nil {
		return err
	}

	err = r.store.set(b.rec.Name, func(s *record) {
		s.State, s.Accepted, s.Rounds, s.AcceptedAt, s.Audited = queued, true, 0, tip, tip
	})
	if err != nil {
		return err
	}

	return r.landAccepted(ctx, b)
}

// landAccepted lands the queued landing of b's accepted spec or fix, in its record's lane. While another kiln holds
// that lane, that kiln lands a spec's. The next kiln monitor run lands a fix's.
func (r *Repo) landAccepted(ctx context.Context, b *build) error {
	name := b.rec.Name
	err := r.run(ctx, b.out, laneOf(&b.rec), name)
	if held, ok := errors.AsType[*laneHeld](err); ok {
		fmt.Fprintf(b.out, "%s: accepted; %s lands it\n", name, landsAccepted(held))
		return nil
	}

	if err != nil {
		return err
	}

	k, err := r.store.read()
	if err != nil {
		return err
	}

	if s := k.find(name); s != nil && s.State == stopped {
		err = fmt.Errorf("the landing of %s stopped: %s", name, landAgain(*s))
	}

	return err
}

// landAgain says what the developer runs on the stopped landing of rec. No kiln accept lands a fix with no session
// that conflicts.
func landAgain(rec record) string {
	if conflicted(rec) {
		return fmt.Sprintf("main changed what the fix changed, so kiln reject %s drops it", rec.Name)
	}

	return fmt.Sprintf("kiln accept %s lands it again, and kiln reject %s drops it", rec.Name, rec.Name)
}

// landsAccepted names the kiln that lands an accepted landing, queued while held refused its lane.
func landsAccepted(held *laneHeld) string {
	switch {
	case held.lane == monitorsLane:
		return "the next kiln monitor run"
	case held.holder == acceptHolder:
		return "the kiln accept that is landing"
	}

	return "the kiln fire that is building"
}

func (r *Repo) tree(name Spec) git.Worktree {
	return git.Worktree(filepath.Join(r.trees, string(name)))
}

// auditTree is the worktree the audit of name's build reads, detached at the build's commit. A spec's name never
// starts with a dot, so it is no build's tree.
func (r *Repo) auditTree(name Spec) git.Worktree {
	return git.Worktree(filepath.Join(r.trees, ".audit-"+string(name)))
}

// gateTree is the worktree where the gate of name's build runs, detached at the commit it judges.
func (r *Repo) gateTree(name Spec) git.Worktree {
	return git.Worktree(filepath.Join(r.trees, ".gate-"+string(name)))
}

func short(commit git.Rev) string { return string(commit[:min(12, len(commit))]) }

func indent(text string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\n  ")
}

// newSession returns a new session ID, a random UUID.
func newSession() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// shellQuote quotes s for /bin/sh, with which Claude Code and git run a hook's command.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+,:@%=") == "" {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
