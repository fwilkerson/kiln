// Package kiln builds the specs a developer approved, and lands them on main.
//
//	r, err := kiln.Open(ctx, ".", os.Environ())
//	err = r.Init(ctx, os.Stdout, kiln.InitOptions{})  // .kiln/ from the preset your files match, or up to date
//	err = r.Queue(ctx, "x", os.Stdout)        // queues .kiln/specs/x.md, as main holds it
//	err = r.Fire(ctx, os.Stdout)              // builds each queued spec, one at a time
//	err = r.Status(ctx, os.Stdout)
//	err = r.TUI(ctx, "", os.Stdin, os.Stdout) // every build, live and past, in a full-screen view
//	err = r.Accept(ctx, "x", os.Stdout)       // takes the work: lands its amendments, and retires the spec
//	err = r.Reject(ctx, "x", os.Stdout)       // turns the amendments away: drops the build, and its branch
//	err = r.MonitorAdd("bugs", os.Stdout)     // copies the bugs monitor kiln ships into .kiln/monitors/bugs/
//	err = r.MonitorRun(ctx, "", os.Stdout)    // runs each monitor in .kiln/monitors/ that is due
//	err = r.Doctor(ctx, "", os.Stdout)        // each problem in the working tree's setup, with its fix
//	names, err := r.Takes(ctx, "queue")       // the specs kiln queue would not refuse
//	err = r.Ledger(os.Stdout, kiln.LedgerOptions{Kind: "gate"}) // each gate, one JSON object a line
//
// # Specs
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
//	                            └─ passes, "accept": true ───────▶ waiting on its branch
//	                                                               ├─ reject ──▶ (no record)
//	                                                               └─ accept ──▶ landing an accepted fix
//	                                                                             ├─ lands ──▶ (no record)
//	                                                                             ├─ stops ──▶ accept or reject again
//	                                                                             └─ a conflict, or a failed gate,
//	                                                                                with a session ──▶ back to the
//	                                                                                session, as a round
//
// kiln queue refuses a fix's record, and a spec that shares a monitor's name.
//
// kiln queue takes only a spec whose Status, at main's tip, is Approved. Its form follows /shape's template, as kiln
// embeds it. Its ## sections all appear, in the template's order, with any of the spec's own between them. Each ###
// N. Title under Acceptance tests holds a Given, a When and a Then, numbered from 1.
//
// # Files
//
// A project tells kiln how its builds run in .kiln/, on main. kiln init writes it from a preset, and brings it up to
// date, as Presets says:
//
//	.kiln/kiln.json             the gate, its rounds, the hosts a session reaches, commands that print writable paths,
//	                            the MCP servers a session calls, the gauges, the workflow, where the acceptance
//	                            tests live, the harness, the paths the audit reads again, and the program that
//	                            receives each notification
//	.kiln/kiln.schema.json      kiln.json's JSON Schema, for an editor, which kiln init writes over each time
//	.kiln/specs/<name>.md       a spec, and <name>.amend.md, its amendments
//	.kiln/init.json             what kiln init and kiln monitor add last wrote, which kiln init merges edits against
//	.kiln/agents/build.md       the build agent
//	.kiln/agents/verify.md      how the acceptance tests are written, and a split build's verify agent
//	.kiln/agents/audit.md       the audit
//	.kiln/reviewers/            the agents /shape runs on a spec before it is approved, which no build runs
//	.kiln/skills/shape/         /shape, which writes a spec with the developer, and its template
//	.kiln/gauges/               the scripts a gauge's command runs
//	.kiln/monitors/<name>/      a monitor: its scripts, its agent and its settings, out of a fix's reach
//
// A command that refuses kiln.json, an agent's file or a spec prints each problem on a line, with its fix. kiln
// doctor names each in the working tree, before a commit, as Doctor says.
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
// A script is a command kiln runs from the project's files. That is each command of the gate, writable and a gauge,
// each script of a monitor, and the notify program. $KILN holds the path of the kiln that runs each. A script runs
// "$KILN", since the PATH of a launchd job or a terminal may name another kiln, or none.
//
// # Presets
//
// kiln init writes .kiln/ from a preset, the part of it that differs by language. kiln ships go, python and
// typescript. A preset of your own is a directory laid out as one, named by a path that starts with . or /. A relative
// path is from the working directory. Any other name is one kiln ships. kiln init records the preset in
// .kiln/init.json, and takes it again when the developer names none. It records a directory by its path from the
// checkout's top. With none named or recorded, it picks the one built-in preset whose detect files the checkout's top
// holds. It says which it picked, and by which files. It refuses when none matches, or more than one, and names each
// one's files. A preset's directory holds:
//
//	kiln.json         the project's kiln.json, which kiln init writes where the checkout has none
//	build-checks.md   the text under "How this project checks its work" in .kiln/agents/build.md
//	verify-tests.md   the text under "Acceptance tests" in .kiln/agents/verify.md
//	audit-tests.md    the text under "Acceptance tests" in .kiln/agents/audit.md
//	detect            a built-in preset's alone: the files that mark a repository as its own, split by white space
//
// Each Markdown file replaces the marker line of its name, without .md, in the default agent's file. The marker for
// build-checks.md is <!-- kiln:preset build-checks -->, below its section's "Tailor this" comment. A preset's kiln.json
// may lack $schema and rounds. kiln init then adds "$schema": "./kiln.schema.json" and "rounds": 3, as for a built-in
// preset. Before it writes anything, kiln init refuses a preset it does not know or cannot use. It refuses a reviewer
// Claude Code could not run then too. Such a reviewer's frontmatter has no description, or a name other than its
// file's, without .md. It refuses the checkout's kiln.json then, where kiln could not use it.
//
// Run again, kiln init brings .kiln/ up to date with the kiln that runs it. It writes each default .kiln/ lacks, but a
// reviewer the project deleted from .kiln/reviewers/. A reviewers folder holding only a .gitkeep keeps no reviewer. A
// file the project left as kiln init last wrote it takes the new default. Into a file the project changed, git
// merge-file merges the change between the last default and the new one. It leaves each conflict between the usual
// markers, and names the file. With no record of the last default, each difference from the new one is a conflict.
// kiln init writes kiln.schema.json over, since kiln owns it. It records each default of the preset, and the preset, in
// .kiln/init.json. It stages and commits nothing. kiln.json is the project's settings: its gate, and what a session
// reaches. A merge could change either unread, so kiln init never merges kiln.json. A kiln.json that is there gains
// only $schema and rounds, where it lacks them. kiln init brings each monitor kiln monitor add recorded up to date the
// same way, as Monitors says.
//
// kiln init links /shape, and each reviewer's file in .kiln/reviewers/, into .claude/. It links each where .claude/
// holds nothing of its name, or a link that reaches nothing. It replaces a link to a reviewer's old file in
// .kiln/agents/, and says that file is unused. It removes each link in .claude/agents/ to a reviewer's file that is
// gone. It says so when the repository has no branch main, which each command that reads main then refuses. It prints
// kiln.json's gate, the commands the pre-commit hook runs on each commit to main. The hook refuses a commit to main
// that fails them. Last, kiln init installs the hook, or says why it installed none.
//
// # Agents
//
// kiln queue reads the build, verify and audit agents' files at the commit it queues. It refuses one it can't use,
// such as one that holds a conflict's marker.
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
// to write the tests. Such a line is an older default's heading for them, or its line on what the build writes. Or
// it names a path's part before {spec}, where the part starts a path.
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
//	           none while the build changes nothing outside .kiln/; it runs once the build does
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
// never excused. With JUnit XML, a run cut short fails too: each test run at main must run at the build's commit. kiln
// keeps the last 16 KiB of each command's output. It runs them at main again only once main, the commands, or the paths
// a split build removes there change. The gate holds commands.lock, with the check or gauge it reads, so no gate runs
// beside another. Its line in the ledger starts once it holds the lock, so a build's time counts no wait.
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
// A monitor is a directory under .kiln/monitors/, named for the monitor. kiln monitor run runs it at main's tip once
// its interval has passed. kiln reads the files below, each optional, and runs each script by its path:
//
//	.kiln/monitors/deps/
//	  check          finds a problem: its last line of stdout is {"fix": true|false, "summary": "..."}
//	  gauge          finds a problem by a number in place of a check: the monitor's gauge, with min or max its line
//	  command        fixes it with no model, outside the sandbox
//	  agent.md       fixes it in a session, from an agent's file, as Agents says
//	  monitor.json   {"hours": 1, "gates": false, "accept": true, "tests": true, "min": 80}
//
// A helper a script runs goes in a subdirectory, such as lib/. Its interval is hours, a whole number from 0 to 65535,
// or 24 when unset. 0 runs it at each kiln monitor run. kiln refuses a file directly under .kiln/monitors/, and a
// monitor whose name no spec could have. It refuses a file directly in a monitor's directory that is none of the five
// above. It refuses a key it does not know, and a value of the wrong type. It refuses a check beside a gauge, a gauge
// with no line, and a line with no gauge. It refuses a check, gauge or command git holds without the executable bit. It
// refuses a monitor with no command and no agent.md. It refuses a monitor that may not write the tests while kiln
// cannot guard them. acceptance then names no path, or one with no part before {spec}. It refuses a gauge whose
// monitor's name a gauge in kiln.json has. A refusal holds back its one monitor: kiln monitor run runs the rest, and
// says why. It notifies when it first refuses that monitor's directory, and again each time the directory changes. kiln
// status and kiln tui show it refused, with why. kiln monitor run with its name refuses, and says why.
//
// kiln monitor add copies a monitor into .kiln/monitors/<name>/, for the developer to commit on main. It takes the
// name of one kiln ships, such as bugs. One that needs a language's tools sits under its preset's name, such as
// go/docs. It takes a directory too, by a path that starts with . or /. The monitor's name is the last element of
// either. Before it writes, it refuses a checkout with no kiln.json, and a monitor kiln could not run by that
// kiln.json. It refuses an agent.md kiln could not read, a name already there, and an unreadable record of kiln
// init. It writes each script kiln runs with the executable bit. It records the monitor in .kiln/init.json, with its
// source. It records a directory by its path from the checkout's top. kiln init brings the monitor up to date from
// that source. It writes a file the source gains as kiln monitor add would. A monitor whose directory the project
// deleted stays deleted, and leaves the record. One whose source kiln init cannot read stays as it is, and kiln init
// says why.
//
// One kiln monitor run:
//
//	lock       monitors.lock, the monitors' lane, beside any kiln fire; build.lock shared
//	recover    the monitors' lane alone, as Lanes says: drop each fix a kiln cut short, or end it landed when main
//	           holds its landing; then land each fix's accepted landing that is queued, but no spec's
//	each monitor main's tip holds, by name, or the one named:
//	  due?     skipped unless due, or named
//	  held?    held while a fix of it builds, waits or lands; held by its last fix, as below, unless named
//	  check    run in <trees>/.monitor-check at main's tip, or its gauge there; no finding: healthy; it holds
//	           commands.lock from before it reads main's tip, so it runs at the main its fix starts from
//	  fix      a build with no spec on kiln/<name>, from main's tip
//	           command   first, outside the sandbox, then the gate; fails with no agent: stopped
//	           agent     while the gate fails, or with no command; kiln.json's rounds, and the audit
//	  ends     landed, waits on its branch, unchanged, needs you, session failed, usage limit, stopped, couldn't run
//	  notify   the developer, as Notifications says
//	  gauges   read after a landing, as after a build's
//	  again    when the check asked to drain, after a fix that landed: from check, due or not, until no fix lands
//
// A monitor is due when it never ran, or its check or gauge couldn't run but did not hang. It is due when a usage
// limit ended its fix, its fix was cut short, or kiln refused it. Otherwise it is due once its last run started its
// interval, less 30 minutes, ago. $KILN_NOW, as RFC 3339, stands for the clock. A monitor with neither check nor gauge
// starts a fix each run it is due.
//
// A check that exits non-zero, or prints no verdict last, couldn't run. kiln then keeps its last line of stdout and
// stderr, but for a check past the timeout, as Hangs says. A verdict that adds "drain": true asks kiln to check again
// at once, once the fix lands. The agent's session has the role fix. It reads what the check printed above its last
// line, and what the gate printed. Then it reads "Fix what the monitor <name> found, as your instructions say, and end
// your turn." The gate adds the check, unless gates is false, and fails while it asks for a fix. A fix that passes
// lands, or waits for kiln accept when accept is true. In an accepted fix's landing, a conflict with main or a failed
// gate goes back to the fix's session. Each spends a round. Before a failed gate goes back, kiln merges main into the
// branch. A fix with no session stops instead. Work that changes nothing ends unchanged. An agent's reply whose last
// line is {"ask": true, "summary": "..."} ends the fix needs you, with its report. One whose last line is {"title":
// "type(scope): summary"}, of at most 72 characters, titles the commit that lands the fix. A landing keeps the title of
// the fix the developer accepted. kiln status shows a landed fix's title as its summary, but for a gauge's. A failed
// session ends it session failed. A usage limit ends it at once, never waiting for the reset. In a landing, each of
// these stops it and keeps the fix. So does work that changes nothing on main. Only a fix whose agent ran gets the
// audit. A landing's audit reads each file whose own change since main differs from the accepted commit's. It runs when
// the work so differs from the commit the audit last read, outside .kiln/. It skips the session's answer to its
// findings, but for a path kiln.json's reaudit names. A landing whose turn kiln cut short resumes that turn first.
//
// A monitor's gauge is a gauge named for the monitor, which kiln reads after each landing too, as Gauges says. The
// monitor's run reads it over the whole repository. It reads in the check's worktree at main's tip, with git's empty
// tree in $KILN_BASE. A reading that crossed its line starts a fix, and one on its line is healthy. A failed command,
// or no number, couldn't run. The reading is the monitor's summary, and its ledger line holds it. The agent reads what
// the gauge printed above its number. At each gate of the fix, unless gates is false, kiln reads the gauge again. Its
// line is the monitor's at main's tip. It reads in the check's worktree at the fix's commit, never the fix's own
// worktree. $KILN_BASE is then the fix's merge base with main. The gate fails while that reading crosses the line, or
// the command fails. Either goes back to the agent, but for a command that ran past kiln.json's timeout, as Hangs says.
// A reading of no number never crossed, so a fix that deletes what crossed lands.
//
// A monitor is held, and starts no fix, by its last fix's outcome. A change since that fix started releases it:
//
//	last fix                                held until a change to
//	stopped, or rejected                    main's tip, any file in the monitor's directory, or what it printed
//	needs you, with a check or a gauge      its directory, or what it printed
//	needs you, with neither                 main's tip, or its directory
//	unchanged, with a check or a gauge      its directory, or what it printed
//	landed, with a gauge                    its directory, or what its gauge printed
//	landed, with a check out of the gate    what its check printed
//	any other                               not held
//
// kiln monitor run with a name skips that hold, but never starts a second fix.
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
// No hang holds a lane, commands.lock or the launchd job forever. kiln.json's timeout, a Go duration read at main's
// tip, bounds two waits. It is 1 hour when unset:
//
//	a command kiln runs for the project    the gate's, a monitor's check or command, a gauge, a writable command
//	a session                              how long it prints nothing
//
// A session that keeps printing is no hang, and the timeout never ends it. kiln kills a command past the timeout
// with its process group, and a session with each process it started. Its words name what ran past, the timeout and
// the setting:
//
//	what ran past                                          ends in
//	a gate's command, a writable command, a monitor's      a stopped build or fix, with those words as its why
//	command, a gauge in a fix's gate, or a session
//	a monitor's check, or its gauge at main's tip          couldn't run, with those words as its summary
//	a gauge read after a landing                           unknown, with those words as why
//
// The timeout counts only the time the Mac is awake, as Go's timers do. It never sends work back as a round. A round
// would wait out the same hang again, holding the same lock. So a monitor whose check or gauge ran past it waits for
// its interval. Then it is held, and runs no check, until main's tip, or any file in the monitor's directory, changes.
// kiln monitor run with its name runs it anyway. The notify program gets 30 s, as Notifications says, since a notice
// holds its kiln's lane. A pause for a usage limit lasts until the limit resets. A wait for commands.lock lasts as long
// as the bounded work that holds it. kiln precommit reads the timeout from the working tree, as it reads the gate. A
// command past it refuses the commit.
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
// # kiln ledger
//
// kiln ledger prints the ledger for a script, one JSON object a line, oldest first by at:
//
//	kiln ledger --kind build --last 10 | jq -s 'map(.cost_usd // empty)'
//
// Its options keep one kind, the lines of one name, or the last few. A name keeps a spec's builds and a monitor's
// runs and fixes. A spec and a monitor that held one name in turn print together. It reads the one ledger that every
// worktree shares. It takes no lock: kiln appends each line whole, and kiln ledger skips a line cut short.
//
// Each object holds kind, then the fields its kind lists below, in that order. A time is RFC 3339, in UTC, to the
// second, as jq's fromdate reads it. A field whose type allows null holds null while kiln does not know it:
//
//	build                          a spec's build, or a monitor's fix
//	  build         string         its ID, which each of its lines names
//	  name          string         its spec, or the fix's monitor
//	  fix           bool           whether it is a monitor's fix
//	  at            time           when it started
//	  size          number         its spec's acceptance tests, or 0 for a fix
//	  outcome       string|null    how it ended, as its last outcome line says; null while it builds
//	  seconds       number|null    its runs' time plus its gates'
//	  cost_usd      number|null    its runs' cost
//	  turns         number|null    its runs' turns
//	  failed_calls  number|null    its runs' failed calls
//	  rounds        number|null    the highest round a run of it started in, or 0 when no failure went back
//	  audits        number         the audit's reads of its work; a retry is no new read
//	  findings      number         the reads whose findings went back to its sessions
//	run                            a session run
//	  build         string         its build's ID
//	  name          string         its build's name
//	  role          string         its session's role, such as build, verify or audit
//	  session       string         its Claude Code session's ID
//	  at            time           when it started
//	  end           time|null      when it ended
//	  seconds       number|null    its time
//	  ended         string|null    how it ended: result, no result or usage limit
//	  cost_usd      number|null    its cost
//	  turns         number|null    its turns
//	  failed_calls  number|null    its failed calls
//	gate                           a run of the gate
//	  build         string         its build's ID
//	  name          string         its build's name
//	  at            time           when it started, once it held commands.lock
//	  end           time           when it ended
//	  seconds       number         its time
//	  passed        bool           whether it passed; a gate that stopped on an error did not
//	  landing       bool           whether it gated a landing, the work rebased onto main
//	  ran_main      bool|null      whether it ran the commands at main too, rather than read the checks kept there
//	reading                        a gauge's reading of a landing
//	  build         string         the landed build's ID
//	  name          string         the landed build's name
//	  gauge         string         the gauge's name
//	  at            time           when kiln read it
//	  landed        string         the commit it read
//	  value         number|null    the reading
//	  crossed       bool           whether it crossed its line
//	monitor                        a monitor's run
//	  name          string         the monitor's name
//	  at            time           when kiln wrote it, once its check or gauge ended
//	  main          string         main's tip as it started
//	  found         string         what it found: healthy, couldn't run, held, fix or refused
//	  build         string|null    the ID of the fix it started
//	  summary       string         its check's summary, or why it couldn't run, or why kiln refused it
//	  gauge         string|null    the gauge it read in place of a check
//	  value         number|null    that gauge's reading
//	  crossed       bool           whether that reading crossed the gauge's line
//
// A sum, such as a build's cost, is null while one of its runs lacks its part. A run lacks its time and failed calls
// until it ends. A build's rounds are null while a run's start line does not say why it started. A gate's ran_main is
// null for a gate older than the field, or one that stopped on an error first. A build's outcome is landed, stopped,
// waits on its branch, dropped or accepted. A fix's may also be needs you, session failed, usage limit, couldn't run,
// unchanged or rejected.
//
// # Gauges
//
// A gauge is a command kiln.json names, which prints a number, and a line: min or max, never both. A monitor's gauge
// is one too, as Monitors says.
//
//	{"name": "mutation", "command": ".kiln/gauges/mutation-score \"$KILN_BASE\"", "min": 80}
//
// After each landing of a build or a fix, kiln reads each gauge at main's tip: kiln.json's, then the monitors'. It runs
// each command in turn with sh -c, in <trees>/.gauges at the landed commit. $KILN_BASE holds main's tip before the
// landing, so a command can measure one build's work alone. In a monitor's run, $KILN_BASE is git's empty tree. There
// git diff "$KILN_BASE" HEAD names every file, and git log "$KILN_BASE"..HEAD fails. The worktree holds only the
// commit's files. So a command that reads the changed files should list them with git diff --diff-filter=d. The reading
// is the last line of stdout, trimmed, as a decimal number. A command that fails, or prints no number last, reads
// unknown. One that exits 0 and prints nothing read nothing: kiln says so, and writes no reading. So kiln status keeps
// showing the last reading kiln took, with its landing. kiln says why, by the last line of its stderr, or of its
// stdout. Hangs says why for a command past the timeout. kiln writes each reading to its ledger with its line, and
// judges it against that line wherever it shows. A reading below min, or above max, crossed its line. An unknown one
// never did. The kiln that landed the build prints the readings after its summary, and only then starts the next build.
// A gauge never blocks a landing, and nothing reads a landing kiln missed. The read holds commands.lock, so it waits
// for a gate, and a gate waits for it.
//
// # Notifications
//
// kiln notifies the developer when a spec's build or a monitor's fix needs them. A spec's build notifies once it ends
// stopped, waiting for UAT, or waiting on its branch, whichever kiln built it. A fix notifies once kiln monitor run
// ends it landed, waiting on its branch, stopped, needs you, or session failed. A session that failed as the
// monitor's last run did notifies nothing, so an expired sign-in notifies once per monitor. kiln monitor run notifies
// each landing it takes up as it starts, or finds on main. A fix's landing that kiln accept makes notifies nothing,
// since the developer reads its output.
//
// kiln.json's notify names the program that receives each notification. kiln reads it at main's tip:
//
//	{"notify": ".kiln/notify"}
//
// kiln runs it with sh -c in <top>, outside the sandbox, with kiln's environment, as it runs a gauge. The program's
// file is the one that checkout holds, so kiln notifies with no lock and no checkout. $KILN_NOTICE holds the notice,
// one JSON object:
//
//	{"kind": "spec", "name": "x", "state": "waiting for UAT",
//	 "summary": "4.2 min · $0.42 · 3 turns · 1 failed call\n0 of 1 round · …",
//	 "next": "read it with git show 3f9c2ab81d0e, then run kiln accept x, or …",
//	 "top": "/Users/me/scratch", "commit": "3f9c2ab81d0e…"}
//
//	kind     spec, for a spec's build, or monitor, for a monitor's fix
//	name     the spec's name, or the monitor's
//	state    where it stands, as kiln status words it, such as stopped, or waiting on the developer
//	summary  the lines kiln prints below that, but the next step; a stopped spec's starts with why it stopped
//	next     what to run next, as kiln prints it after "next:"; "" when the developer has nothing to run
//	top      the top directory of the checkout kiln runs in
//	commit   the full hash of the commit that landed the build or the fix on main; absent when none did
//
// What the program prints goes to kiln's output. kiln waits 30 s for it, then kills it and what it started. A program
// that fails, runs past that, or is missing changes no build's or fix's result. kiln says why in its output.
//
// Without notify, kiln prints each notification in its output as OSC 9, which a terminal such as Ghostty shows. Its
// body is "kiln: <name> <state>". kiln prints it whether or not the output is a terminal. It prints it too when kiln
// cannot read kiln.json at main's tip, or the program fails. So no notification is lost. The job of kiln monitor
// install writes kiln's output to monitor.log, where no terminal shows OSC 9.
//
// A spec's build prints its summary after it notifies, from kiln's record, its ledger and git:
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
	"os/exec"
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

// noMainRefusal says a repository has no branch main, or no commit on it, and how to make one.
var noMainRefusal = refusal{"kiln builds on the branch " + mainBranch + ", which this repository lacks",
	"commit to " + mainBranch + ", or run git branch -m " + mainBranch}

// noMain is noMainRefusal on one line, as kiln init prints it.
var noMain = noMainRefusal.String()

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
// withholds. It refuses a dir that no git repository holds.
func Open(ctx context.Context, dir string, env []string) (*Repo, error) {
	top, err := plainGit(ctx, dir, proc.Git.Env(env), "rev-parse", "--show-toplevel")
	if _, ran := errors.AsType[*exec.ExitError](err); ran {
		// git found no repository there, and its own words name its search, not the fix.
		return nil, fmt.Errorf("%w: %s is in no git repository: run git init there, or run kiln in a repository",
			errRefused, dir)
	}

	if err != nil {
		return nil, err
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
		claude: claude.New(claude.Config{Program: claudeProgram, Env: env, Sessions: filepath.Join(kept, "sessions")}),
	}, nil
}

// claudeProgram is the claude that runs each session, found on the PATH of the environment Open was given.
const claudeProgram = "claude"

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
	set, err := plainGit(ctx, r.top, proc.Git.Env(r.env), "config", "--default", "", "--get", "core.hooksPath")
	if err != nil {
		return err
	}

	hooks, err := r.hooksDir(ctx)
	if err != nil {
		return err
	}

	if set != "" {
		whose := "which other repositories may share"
		if strings.HasPrefix(hooks, r.top+string(filepath.Separator)) {
			whose = "this repository's own hooks directory"
		}

		fmt.Fprintf(out, "installed no pre-commit hook, since core.hooksPath names %s, %s; add this line to your "+
			"own pre-commit hook: %s\n", set, whose, precommitLine)
		return nil
	}

	hook := filepath.Join(hooks, "pre-commit")
	if old, err := os.ReadFile(hook); err == nil && !bytes.Contains(old, []byte(hookMark)) {
		fmt.Fprintf(out, "installed no pre-commit hook, since %s is another one; add this line to it: %s\n", hook,
			precommitLine)
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

// precommitLine is the line a developer's own pre-commit hook adds, to run the gate as kiln's hook does.
const precommitLine = "kiln precommit"

// hooksDir is the directory whose hooks git runs: the one core.hooksPath names, or the repository's own.
func (r *Repo) hooksDir(ctx context.Context) (string, error) {
	return plainGit(ctx, r.top, proc.Git.Env(r.env), "rev-parse", "--path-format=absolute", "--git-path", "hooks")
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

// misnamed refuses a name no spec could have, before kiln reads a file by it.
func (s Spec) misnamed() refusals {
	if specName.MatchString(string(s)) {
		return nil
	}

	return refusals{{fmt.Sprintf("%q is not a spec's name", s),
		"name it in lower-case letters, digits and hyphens, such as checkout-flow"}}
}

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
// follows the template. Main must hold a kiln.json, a build agent, a verify agent and an audit agent that kiln can
// use.
func (r *Repo) queueable(ctx context.Context, main git.Rev, name Spec) ([]byte, error) {
	if err := r.ready(ctx, main); err != nil {
		return nil, err
	}

	spec, err := r.approvedSpec(ctx, main, name)
	if err != nil {
		return nil, err
	}

	c, err := r.config(ctx, main)
	if err != nil {
		return nil, err
	}

	monitors, err := r.git.Files(ctx, main, monitorsDir)
	if err != nil {
		return nil, err
	}

	if refused := c.specRefusals(name, spec, monitors); len(refused) > 0 {
		return nil, fmt.Errorf("%w: %w", errRefused, refused)
	}

	return spec, nil
}

// specRefusals say why kiln queue refuses the spec name, whose text is spec, but for its Status. They are its form,
// its acceptance paths against the harness, and a monitor of its name. monitors are the names under .kiln/monitors/.
func (c config) specRefusals(name Spec, spec []byte, monitors []string) refusals {
	refused := c.harnessRefusals(name)
	if slices.Contains(monitors, string(name)) {
		refused = append(refused, refusal{fmt.Sprintf("%s holds a monitor %s, and one name is never both a "+
			"spec's and a monitor's", monitorsDir, name), "rename one"})
	}

	return append(refused, formOf(string(spec)).about(name.file()+" leaves /shape's template:")...)
}

// ready refuses while main, at main, holds no kiln.json, build agent, verify agent or audit agent that kiln can use.
func (r *Repo) ready(ctx context.Context, main git.Rev) error {
	c, err := r.config(ctx, main)
	if err != nil {
		return fmt.Errorf("%w: %w", errRefused, err)
	}

	refused, err := c.readiness(ctx, r.filesAt(main))
	if err != nil {
		return err
	}

	if len(refused) > 0 {
		return fmt.Errorf("%w: %w", errRefused, refused)
	}

	return nil
}

// readiness says why kiln queue refuses c, with the build, verify and audit agents that f holds. c must name where
// the acceptance tests live. The build agent must not say how to write them. It answers an error only when f could
// not be read.
func (c config) readiness(ctx context.Context, f files) (refusals, error) {
	refused := c.acceptanceRefusals()
	var build agent
	for _, role := range []string{buildRole, verifyRole, auditRole} {
		a, err := f.agent(ctx, agentFile(role), initHint)
		if err := refused.add(err); err != nil {
			return nil, err
		}

		if role == buildRole {
			build = a
		}
	}

	if line := testsText(build.body, c.fixedParts()); line != "" {
		refused = append(refused, refusal{fmt.Sprintf("%s says how to write the acceptance tests, in %q",
			agentFile(buildRole), line), fmt.Sprintf("move that text to %s, which every build reads",
			agentFile(verifyRole))})
	}

	return refused, nil
}

// testsText is the first line of body that says how to write the acceptance tests, or "". An older default build
// agent said so. Such a line is its heading, or its line on what the build writes. Or it names one of fixed, where
// the tests live.
func testsText(body string, fixed []string) string {
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(line)
		// A part of "" is no path's, so it says nothing about where the tests live.
		names := func(part string) bool { return part != "" && startsPath(line, part) }
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
	if refused := name.misnamed(); refused != nil {
		return nil, fmt.Errorf("%w: %w", errRefused, refused)
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

// Takes answers the names command takes, for the shell to complete. For queue, accept and reject, they are specs, and
// for accept and reject, waiting fixes too. For tui, they are the specs kiln holds or its ledger holds. For "monitor
// run" and "monitor show", they are the monitors at main's tip, refused or not. kiln monitor run then says why it
// refuses one. While kiln refuses kiln.json there, they are none. For "monitor add", they are the monitors kiln ships
// that fit the preset .kiln/init.json records, as ShippedMonitors says. Each whose directory the checkout holds already
// is left out. For doctor, they are the specs the working tree holds, each by a name a spec may have. Other commands
// take none.
func (r *Repo) Takes(ctx context.Context, command string) ([]string, error) {
	switch command {
	case "tui":
		return r.viewed(ctx)
	case "doctor":
		return r.treeSpecs()
	case acceptCommand, rejectCommand:
		return r.taking(command)
	case queueCommand:
		return r.queueNames(ctx)
	case "monitor run", "monitor show":
		return r.monitorNames(ctx)
	case "monitor add":
		rec, err := r.lastInit()
		return r.unadded(fitting(rec.Preset)), err
	}

	return nil, nil
}

// specFile is the spec whose file under .kiln/specs/ is named file. It answers false for an amendment's file, and a
// file whose name no spec could have.
func specFile(file string) (Spec, bool) {
	name, ok := strings.CutSuffix(file, ".md")
	return Spec(name), ok && !strings.HasSuffix(file, amendSuffix) && Spec(name).misnamed() == nil
}

// treeSpecs answers the specs the working tree holds, by a name a spec may have, but none of their amendments.
func (r *Repo) treeSpecs() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(r.top, specsDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	var names []string
	for _, e := range entries {
		if name, ok := specFile(e.Name()); ok {
			names = append(names, string(name))
		}
	}

	return names, err
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

// monitorNames answers the monitors main's tip holds, refused or not, since kiln monitor run says why it refuses one.
func (r *Repo) monitorNames(ctx context.Context) ([]string, error) {
	main, err := r.mainTip(ctx)
	if err != nil {
		return nil, err
	}

	_, reads, err := r.monitors(ctx, main)
	names := make([]string, len(reads))
	for i, d := range reads {
		names[i] = string(d.name)
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

// Status shows each spec kiln holds, and where it stands, but no monitor's fix. Then it shows each gauge at main's tip,
// with its latest reading. It shows each monitor main's tip holds, and where it stands, unless kiln refuses kiln.json
// there. Last, it says when .kiln/ at main's tip lacks kiln init's record, or is behind what kiln init writes.
func (r *Repo) Status(ctx context.Context, out io.Writer) error {
	main, err := r.mainTip(ctx)
	if err != nil {
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

	if err := r.monitorStatus(ctx, out); err != nil {
		return err
	}

	return r.initStatus(ctx, main, out)
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
