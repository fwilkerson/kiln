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
//	err = r.Tend(ctx, "x", kiln.Terminal{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}) // a session of yours, then on
//	err = r.MonitorAdd("bugs", os.Stdout)     // copies the bugs monitor kiln ships into .kiln/monitors/bugs/
//	err = r.MonitorRun(ctx, "", os.Stdout)    // runs each monitor in .kiln/monitors/ that is due
//	err = r.Doctor(ctx, "", os.Stdout)        // each problem in the working tree's setup, with its fix
//	names, err := r.Takes(ctx, "queue")       // the specs that kiln queue takes without a refusal
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
//	                                    │            ├─ queue ──▶ queued
//	                                    │            ├─ tend ───▶ queued as it stood, and goes on in its sessions
//	                                    │            └─ tend --landed ──▶ waiting for UAT
//	                                    └─ amends ─▶ waiting on its branch with amendments
//	                                                 ├─ reject ──▶ (no record)
//	                                                 └─ accept ──▶ landing accepted amendments
//	                                                               ├─ lands ──▶ retired
//	                                                               └─ stops ──▶ landing accepted amendments, stopped
//	                                                                            ├─ reject ──▶ (no record)
//	                                                                            └─ accept or tend ──▶ landing again
//
// A spec landing accepted amendments is queued, building or stopped, as the build that lands it stands. Every other
// command refuses. A monitor's fix lives in the book under the monitor's name:
//
//	(no record) ─fix─▶ building ├─ lands, is unchanged, asks, fails, couldn't run or meets a limit ──▶ (no record)
//	                            ├─ stops ──▶ stopped, until its next fix replaces it, or a healthy run drops it
//	                            └─ passes, "mustAccept": true ───▶ waiting on its branch
//	                                                               ├─ reject ──▶ (no record)
//	                                                               └─ accept ──▶ landing an accepted fix
//	                                                                             ├─ lands ──▶ (no record)
//	                                                                             ├─ stops ──▶ accept, reject or tend
//	                                                                             └─ a conflict, or a failed gate,
//	                                                                                with a session ──▶ back to the
//	                                                                                session, as a round
//
// kiln queue refuses a fix's record, and a spec that shares a monitor's name.
//
// A record that kiln reject took refuses kiln queue, accept and tend, until kiln reject drops it. While a live kiln
// accept holds a record, kiln queue, reject and tend refuse it. The accept lets go once it queues the landing,
// retires the spec, or fails.
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
//	                            the MCP servers a session calls, the workflow of a spec's build, where the
//	                            acceptance tests live, the test helpers, the test files, the paths the audit reads
//	                            again, and the program that receives each notification
//	.kiln/specs/<name>.md       a spec, and <name>.amend.md, its amendments
//	.kiln/agents/build.md       the build agent
//	.kiln/agents/verify.md      how the acceptance tests are written, and the agent of a verify session
//	.kiln/agents/audit.md       the audit
//	.kiln/reviewers/            the agents /shape runs on a spec before it is approved, which no build runs
//	.kiln/skills/shape/         /shape, which writes a spec with the developer, and its template
//	.kiln/monitors/<name>/      a monitor: its scripts, its agent and its settings, out of a fix's reach
//	.kiln/generated/            the files kiln owns, which kiln init writes over, so never edit one:
//	  kiln.schema.json          kiln.json's JSON Schema, for an editor
//	  monitor.schema.json       each monitor.json's JSON Schema, for an editor
//	  init.json                 what kiln init and kiln monitor add last wrote, which kiln init merges edits against
//	  .gitattributes            marks each file there as generated, for a tool that reads the attribute
//
// A command that refuses kiln.json, an agent's file or a spec prints each problem on a line, with its fix. kiln
// doctor names each in the working tree, before a commit, as Doctor says.
//
// kiln keeps its own files out of the working tree. Main is the branch named main:
//
//	<git common dir>/kiln/         shared by every worktree of the repository
//	  state.json                   each spec's state, the gate's last checks at main, and the ledger lines of
//	                               its last write
//	  lock                         held while kiln changes state.json
//	  specs.lock                   held by the one kiln at work in the specs' lane, and names it
//	  monitors.lock                held by the one kiln at work in the monitors' lane, and names it
//	  tend-<name>.lock             held by the one kiln tend at work on a spec or monitor, until it exits
//	  commands.lock                held while kiln runs what judges the work or moves main, as Lanes says
//	  worktrees.lock               held while git adds or removes a worktree, which writes the shared config
//	  ledger.jsonl                 each build, run, gate, outcome, gauge reading, audit finding and monitor run
//	  monitor.log                  what the launchd job of kiln monitor install printed
//	  sessions/<id>/               each session's files, as go doc ./internal/claude lists them; a kiln tend
//	                               session's tend.md, and the note.md it leaves for the build
//	<user cache dir>/kiln/         ~/Library/Caches/kiln/
//	  build.lock                   shared by each kiln that drives a build session; HoldBuilds takes it alone
//	  <repo>-<8 hex>/              <trees>, the repository's checkouts, named for its directory and git common dir
//	    <name>/                    a build's or a fix's worktree on kiln/<name>, where its code session works
//	    .audit-<name>/             the audit's worktree, at the build's commit
//	    .gate-<name>/              where the gate runs, at the build's commit
//	    .verify-<name>/            the verify session's clone, in build-and-verify or verify-first
//	    .main/                     where the gate runs at main
//	    .gauges/                   where gauges are read after a landing
//	    .monitor-check/            where a monitor's check runs, or its gauge
//
// kiln removes every checkout a build made once it lands, is accepted, rejected or queued anew. A build that stopped,
// or waits on its branch, keeps them. Only a kiln that holds commands.lock uses .main or .gauges. Only the kiln that
// holds the monitors' lane uses .monitor-check.
//
// A script is a command kiln runs from the project's files. That is each command of gate and writablePathCommands, each
// script of a monitor, and the notify program. $KILN holds the path of the kiln that runs each. A script runs
// "$KILN", since the PATH of a launchd job or a terminal may name another kiln, or none.
//
// # Presets
//
// kiln init writes .kiln/ from a preset, the part of it that differs by language. kiln ships go, python and typescript.
// A preset of your own is a directory laid out as one, named by a path that starts with . or /. A relative path is from
// the working directory. Any other name is one kiln ships. kiln init records the preset in .kiln/generated/init.json,
// and takes it again when the developer names none. It records a directory by its path from the checkout's top. With
// none named or recorded, it picks the one built-in preset whose detect files the checkout's top holds. It says which
// it picked, and by which files. It refuses when none matches, or more than one, and names each one's files. A preset's
// directory holds:
//
//	kiln.json         the project's kiln.json, which kiln init writes where the checkout has none
//	build-checks.md   the text under "How this project checks its work" in .kiln/agents/build.md
//	verify-tests.md   the text under "Acceptance tests" in .kiln/agents/verify.md
//	audit-tests.md    the text under "Acceptance tests" in .kiln/agents/audit.md
//	detect            a built-in preset's alone: the files that mark a repository as its own, split by white space
//
// Each Markdown file replaces the marker line of its name, without .md, in the default agent's file. The marker for
// build-checks.md is <!-- kiln:preset build-checks -->, below its section's "Tailor this" comment. A preset's kiln.json
// may lack $schema and rounds. kiln init then adds "$schema": "./generated/kiln.schema.json" and "rounds": 3, as for a
// built-in preset. Before it writes anything, kiln init refuses a preset it does not know or cannot use. It refuses a
// reviewer Claude Code could not run then too. Such a reviewer's frontmatter has no description, or a name other than
// its file's, without .md. It refuses the checkout's kiln.json then, where kiln could not use it.
//
// Run again, kiln init brings .kiln/ up to date with the kiln that runs it. It writes each default .kiln/ lacks, but a
// reviewer the project deleted from .kiln/reviewers/. A reviewers folder holding only a .gitkeep keeps no reviewer. A
// file the project left as kiln init last wrote it takes the new default. Into a file the project changed, git
// merge-file merges the change between the last default and the new one. It leaves each conflict between the usual
// markers, and names the file. With no record of the last default, each difference from the new one is a conflict. kiln
// init writes over each file in .kiln/generated/, since kiln owns them. It records each default of the
// preset, and the preset, in init.json there. It stages and commits nothing. kiln.json is the project's settings: its
// gate, and what a session reaches. A merge could change either unread, so kiln init never merges kiln.json. A
// kiln.json that is there gains only $schema and rounds, where it lacks them. kiln init brings each monitor kiln
// monitor add recorded up to date the same way, as Monitors says.
//
// kiln init links /shape, and each reviewer's file in .kiln/reviewers/, into .claude/. It links each where .claude/
// holds nothing of its name, or a link that reaches nothing. It removes each link in .claude/agents/ to a reviewer's
// file that is gone. It says so when the repository has no branch main, which each command that reads main then
// refuses. It prints kiln.json's gate, the commands the pre-commit hook runs on each commit to main. The hook refuses
// a commit to main that fails them. Last, kiln init installs the hook, or says why it installed none.
//
// # Agents
//
// kiln queue reads the build, verify and audit agents' files at the commit it queues. It refuses one it can't use,
// such as one that holds a conflict's marker.
// Every session of the build runs with them as they stood there. An agent's file names the session's tools, model,
// effort and advisor in its frontmatter. Without tools, it has every tool. Without model, it runs Opus. Without effort
// or advisor, kiln names none, and each is Claude Code's own. Its body comes first in the session's system prompt.
// Then comes kiln's contract with the agent, which names where the tests live. In the workflow "build", the
// build session writes the tests too. Its prompt then holds the verify agent's body after the build agent's. No file
// changes the contract, the denied commands, the forbidden actions, or the guard on .kiln/. The hosts, writable paths
// and MCP servers are kiln.json's at main's tip, and none without its keys. kiln runs each command that prints paths
// once per kiln command. One that fails stops the build.
//
// kiln queue refuses a kiln.json whose acceptanceTests names no path, a path without {spec}, or one that starts
// with it. It refuses a path that path.Clean changes, but for its last slash, or one that starts with a slash. It
// refuses a list in which one path's part before {spec} starts another's, or equals it, with case folded. It
// refuses a spec when a testHelpers path is one of its paths, or holds one, or lies under one. It refuses a build
// agent that says where the tests live. Such an agent names a path's part before {spec}, where the part starts a path.
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
// the nearest above. Nothing the audit writes reaches the build, and kiln removes its worktree once it ends.
//
// The contract that ends the audit's system prompt sets the form of the audit's reply. Each finding listed is a block
// of named fields. The reply's last line is "read N directories, found M, reported K". While M is 0, nothing goes back
// to the build. Otherwise the whole reply goes back, as does a reply without that line. An empty reply is no result,
// so the audit runs again. The audit run's end line in the ledger holds each finding listed. A reply in another form
// is one finding there, which holds the whole reply. A
// landing that doesn't move main leaves the branch as it stood before the landing began. Should kiln die once main
// holds the landing, the next kiln fire records it landed, and starts no session. kiln accept lands a commit that
// retires the spec. A spec that waited on its branch lands its work first, with the amendments folded in.
//
// A usage limit pauses the build until the limit resets, and then resumes the same session. A limit with no reset
// pauses it for 5 minutes.
//
// kiln.json's "workflow", as main held it when the spec was queued, says how the spec builds. A monitor.json's
// says how its fix runs. Each file takes the same three:
//
//	build              one session writes the code and its tests, as above (the default)
//	build-and-verify   the verify and build sessions start at once
//	verify-first       the build session starts once the verify session's tests are proved
//
// The last two are one isolated workflow, in two orders:
//
//	verify-first       verify ──▶ proof ──▶ join ──▶ build ──▶ gate ──▶ audit ──▶ land
//	build-and-verify   verify ──▶ proof ──┐
//	                   build ─────────────┴▶ join ──▶ gate ──▶ audit ──▶ land
//
//	verify   the verify session, in <trees>/.verify-<name>, a clone of main on kiln/<name>.verify. A spec's writes
//	         its acceptance tests from the spec alone, and alone amends the spec. A fix's writes test files from what
//	         the check printed. It never reads the build's worktree, nor the build session's transcripts.
//	proof    the gate at the verify session's commit, which holds no code of the build's. It must fail, and on tests
//	         alone; a test that does not compile names no failed test. Otherwise its report goes back to the verify
//	         session, as a round. A commit that leaves the proved tests as they were needs no proof, unless it
//	         changes a path the verify session may not write.
//	join     kiln merges the proved commit into kiln/<name>, with main as the verify session's clone holds it; a
//	         conflict goes to the build session, as a round
//	build    the build session, or the fix's, in <trees>/<name>. It reads and runs the proved tests, and changes none.
//	         Its next message after a join opens with what the join brought: the tests' files, the proof's report,
//	         and each new amendment's wording. A join that needs no answer starts no run.
//
// Each session has kiln.json's rounds of its own. kiln commits, proves, joins and gates only between runs. It proves
// and joins only while it owes the verify session nothing. It never gates the build's work before the join. Side by
// side, the build session's first run starts from main. So kiln removes a revised spec's earlier tests before that
// run, and puts them back before the first join. In turn, the build session starts from the
// join. Once a proof stands, the build session runs only while kiln owes the verify session nothing.
//
// A build reply whose last line starts "cannot hold:" goes to the verify session. A fix's session asks for the
// developer instead. The audit's findings go to the verify session first, then to the build session. That session
// reads the verify session's answer, joined. A fix's findings go to its session alone once the developer accepted it.
// The audit's prompt names the tests that failed at the proof, and each testHelpers file the build session changed.
//
// After a proof that did not stand, a verify run that ended with a result may change no path. That stops the build:
// "the verify session changed no test after a proof that did not stand". A run that failed gets the proof's report
// again, as a round. Each gate after the join fails while a file the verify session alone writes differs from the
// proved tests. Those are main's file with the proved commit's change applied. The landing's gate fails so too.
//
// When main moved under the build, kiln merges main into the verify session's clone alone. A conflict there goes to a
// spec's verify session, as a round, and stops a fix. The join then brings main into the work. So a conflict between
// main and the work arises at the join, and costs the build session one round.
//
// # kiln tend
//
// kiln tend <name> opens an interactive Claude Code session on what waits on a repair, in the developer's checkout.
// The session is theirs: their settings and permissions, no sandbox or hook of kiln's. So it may change
// main, the spec, a monitor's own files and the rest of .kiln/. It takes a name as it stands:
//
//	a spec's stopped build, or its stopped landing of accepted amendments
//	a monitor's stopped fix, or its accepted landing that stopped
//	a monitor that needs you, whose session failed, that couldn't run, or that kiln refuses, held or not
//
// A monitor also waits on a repair while kiln cannot read main's monitors, such as for a kiln.json it refuses.
// It refuses any other name, and says where it stands. It refuses while another kiln tend is at work on the name, by
// tend-<name>.lock. The book's record decides a name that is both a spec's and a monitor's. The session's system
// prompt ends with kiln's know-how, which no file of the project changes. Its first message holds these lines, but
// each with nothing to hold:
//
//	The spec <name>: <state>                 or The monitor <name>: <state>, as kiln status words it
//	<why>                                    why the build stopped, and its failing: line; or the summary of the
//	                                         check that started the monitor's fix, then the fix's summary and
//	                                         report; or why kiln refuses it
//	Its work: kiln/<name>, in <worktree>     a stopped record's, while its worktree is there
//	Its sessions: build <id>, in <dir>; ...  each session the record holds, with its checkout
//	Its directory: .kiln/monitors/<name>/    a monitor's
//	A note for the build: <path>             a stopped build's: note.md in the session's directory
//
// A Ctrl-C reaches the session, and kiln ignores it while the session runs. A session that exits non-zero fails kiln
// tend. Otherwise kiln reads where the name stands again, and goes on at once, asking nothing:
//
//	a spec's stopped build     queued as it stood: its build ID, sessions, branch, audit and limit; rounds at 0
//	a stopped landing          lands again, as kiln accept does
//	a monitor                  runs again, as kiln monitor run with its name does; it waits for the monitors' lane
//
// A build that goes on owes each of its sessions a message: why it stopped, and the note's text. When main moved, kiln
// merges main into the build's branch. Beside a verify session, it merges main into that session's clone instead, and
// the join brings main into the branch. A conflict goes back as a round. kiln resumes
// each session once with the message, whichever turn had ended. From there it is the same build, under the same build
// ID. While another kiln holds the specs' lane, that kiln goes on with it, as with an accepted landing. Should it halt
// once taken, it still owes the message, and waits for kiln fire. kiln queue builds it anew instead.
//
// kiln tend <name> --landed records a stopped build's work as landed on main, by commits kiln did not make. The
// session runs it once it finished the work there. It refuses all but a spec's stopped build. It refuses while main
// changed no file under the spec's acceptance paths since the build was queued. Otherwise kiln drops the build's
// checkouts and branches, and the spec waits for UAT at main's tip. It sends no notice and reads no gauge, and no audit
// reads the work.
//
// # The gate
//
// The gate runs kiln.json's commands, read from main's tip, with sh -c at the build's commit and at main. It passes
// when every command exits 0, but for tests that fail at main too. A command names its failed tests in the output of go
// test. Or it writes them as JUnit XML, to the path kiln gives it in $KILN_JUNIT. A failure that names no test is never
// excused. With JUnit XML, a run cut short fails too: each test run at main must run at the build's commit. kiln keeps
// the last 16 KiB of each command's output. It runs them at main again only once main or the commands change. The
// gate holds commands.lock,
// with the check or gauge it reads, so no gate runs beside another. Its line in the ledger starts once it holds the
// lock, so a build's time counts no wait.
//
// The gate also fails on a change to a guarded path, however the path was written. A session's hook refuses an Edit
// or a Write there before it happens. Each name is folded as APFS folds it:
//
//	a .git directory          only git and kiln write there
//	.kiln/                    but .kiln/specs/<name>.amend.md, which only the verify session writes, where there is
//	                          one; a monitor's fix writes nothing there
//	a .claude directory       the developer's Claude Code settings, agents and skills, whose hooks run unsandboxed
//	the other side            with a verify session, the build session writes neither the acceptance tests nor the
//	                          amendments; the verify session writes only those, and the testHelpers paths
//	the acceptance tests      to a monitor's fix, each path starting with an acceptance path's part before {spec},
//	                          unless its monitor says "writesTests": true
//	the test files            to the fix session of a fix with a verify session, and all but them to that session
//
// # Monitors
//
// A monitor is a directory under .kiln/monitors/, named for the monitor. kiln monitor run runs it at main's tip once
// its interval has passed. kiln reads the files below, each optional, and runs each script by its path:
//
//	.kiln/monitors/deps/
//	  check          finds a problem: its last line of stdout is {"fix": true|false, "summary": "..."}
//	  gauge          finds a problem by a number in place of a check: the monitor's gauge, its line atLeast or atMost
//	  command        fixes it with no model, outside the sandbox
//	  agent.md       fixes it in a session, from an agent's file, as Agents says
//	  verify.md      writes the fix's tests in a session apart from agent.md's, as Fixes with a verify session says
//	  monitor.json   {"hours": 1, "recheck": false, "mustAccept": true, "writesTests": true, "workflow": "build",
//	                  "atLeast": 80}
//
// A helper a script runs goes in a subdirectory, such as lib/. Its interval is hours, a whole number from 0 to 65535,
// or 24 when unset. 0 runs it at each kiln monitor run. kiln refuses a file directly under .kiln/monitors/, and a
// monitor whose name no spec could have. It refuses a file directly in a monitor's directory that is none of the six
// above. It refuses a key it does not know, and a value of the wrong type. It refuses a check beside a gauge, a gauge
// with no line, and a line with no gauge. It refuses a check, gauge or command git holds without the executable bit. It
// refuses a monitor with no command and no agent.md, unless the monitor holds a gauge alone to watch. Such a monitor
// fixes nothing, so it refuses recheck, mustAccept, writesTests and workflow in its monitor.json, which steer a fix.
// It refuses verify.md in a monitor whose workflow is build. It refuses a monitor with a verify session that breaks a
// rule of Fixes with a verify session. It refuses a
// monitor that may not write the tests while kiln cannot guard them. acceptanceTests then names no path, or one with
// no part before {spec}. A refusal holds back its one monitor: kiln monitor run runs the rest, and says why. It
// notifies when it first refuses that monitor's directory, and again each time the directory changes. kiln status and
// kiln tui show it refused, with why. kiln monitor run with its name refuses, and says why.
//
// kiln monitor add copies a monitor into .kiln/monitors/<name>/, for the developer to commit on main. It takes the name
// of one kiln ships, such as bugs. One that needs a language's tools sits under its preset's name, such as go/docs. It
// takes a directory too, by a path that starts with . or /. The monitor's name is the last element of either. Before it
// writes, it refuses a checkout with no kiln.json, and a monitor kiln could not run by that kiln.json. It refuses an
// agent.md or verify.md kiln could not read, and a name already there. It refuses an unreadable record of kiln init. It
// writes each script kiln runs with the executable bit. A monitor.json that names no "$schema" gains "$schema":
// "../../generated/monitor.schema.json", the schema kiln init writes, which kiln never reads. It records the monitor in
// .kiln/generated/init.json, with its source. It records a directory by its path from the checkout's top. kiln init
// brings the monitor up to date from that source. It writes a file the source gains as kiln monitor add would. A
// monitor whose directory the project deleted stays deleted, and leaves the record. One whose source kiln init cannot
// read stays as it is, and kiln init says why.
//
// One kiln monitor run:
//
//	lock       monitors.lock, the monitors' lane, beside any kiln fire; build.lock shared
//	recover    the monitors' lane alone, as Lanes says: drop each fix a kiln cut short, or end it landed when main
//	           holds its landing; then land each fix's accepted landing that is queued, but no spec's
//	each monitor main's tip holds, by name, or the one named:
//	  due?     skipped unless due, or named
//	  held?    held while a fix of it builds, waits or lands; held by its last finding, as below, unless named
//	  check    run in <trees>/.monitor-check at main's tip, or its gauge there; no finding: healthy; it holds
//	           commands.lock from before it reads main's tip, so it runs at the main its fix starts from
//	  fix      a build with no spec on kiln/<name>, from main's tip; a monitor that fixes nothing needs you instead
//	           command   first, outside the sandbox, then the gate; fails with no agent: stopped
//	           verify    beside agent.md's, or before it: verify.md's session, as Fixes with a verify session says
//	           agent     while the gate fails, or with no command; kiln.json's rounds, and the audit
//	  ends     landed, waits on its branch, unchanged, needs you, session failed, usage limit, stopped, couldn't run
//	           each printed with its time, cost, turns and failed calls, as a spec's build is
//	  notify   the developer, as Notifications says
//	  gauges   read after a landing, as after a build's
//	  again    when the check asked to drain, after a fix that landed: from check, due or not, until no fix lands
//
// A monitor is due when it never ran, or its check or gauge couldn't run but did not hang. It is due when a usage limit
// ended its fix, its fix was cut short, or kiln refused it. Otherwise, it is due once its last run started its
// interval, less 30 minutes, ago. $KILN_NOW, as RFC 3339, stands for the clock. A monitor with neither check nor gauge
// starts a fix each run it is due.
//
// A check that exits non-zero, or prints no verdict last, couldn't run. kiln then keeps its last line of stdout and
// stderr, but for a check past the timeout, as Hangs says. A verdict that adds "drain": true asks kiln to check again
// at once, once the fix lands. The agent's session has the role fix. It reads what the check printed above its last
// line, and what the gate printed. Then it reads "Fix what the monitor <name> found, as your instructions say, and end
// your turn." The gate adds the check, unless recheck is false, and fails while it asks for a fix. A fix that passes
// lands, or waits for kiln accept when mustAccept is true. In an accepted fix's landing, a conflict with main or a
// failed gate goes back to the fix's session. Each spends a round. Before a failed gate goes back, kiln merges main
// into the branch. A fix with no session stops instead. Work that changes nothing ends unchanged. An agent's reply
// whose last line is {"ask": true, "summary": "..."} ends the fix needs you, with its report. One whose last line is
// {"title": "type(scope): summary"}, of at most 72 characters, titles the commit that lands the fix. A landing keeps
// the title of the fix the developer accepted. kiln status shows a landed fix's title as its summary, but for a
// gauge's. A failed session ends it session failed. A usage limit ends it at once, never waiting for the reset. In a
// landing, each of these stops it and keeps the fix. So does work that changes nothing on main. Only a fix whose agent
// ran gets the audit. A landing's audit reads each file whose own change since main differs from the accepted commit's.
// It runs when the work so differs from the commit the audit last read, outside .kiln/. It skips the session's answer
// to its findings, but for a path kiln.json's reaudit names. A landing whose turn kiln cut short resumes that turn
// first.
//
// A monitor's gauge is named for the monitor, and kiln reads it after each landing too, as Gauges says. The
// monitor's run reads it over the whole repository. It reads in the check's worktree at main's tip, with git's empty
// tree in $KILN_BASE. A reading that crossed its line starts a fix, and one on its line is healthy. A gauge that read
// nothing is healthy too, with no reading, as Gauges says. A failed gauge, or a last line that is no number, couldn't
// run. The reading is the monitor's summary, and its ledger line holds it. The agent reads what
// the gauge printed above its number. At each gate of the fix, unless recheck is false, kiln reads the gauge again. Its
// line is the monitor's at main's tip. It reads in the check's worktree at the fix's commit, never the fix's own
// worktree. $KILN_BASE is then the fix's merge base with main. The gate fails while that reading crosses the line, or
// the gauge fails. Either goes back to the agent, but for a gauge that ran past kiln.json's timeout, as Hangs says.
// A reading of no number never crossed, so a fix that deletes what crossed lands.
//
// A monitor that holds a gauge, and no command or agent.md, watches the gauge and fixes nothing. A crossed reading
// starts no fix: the run ends needs you, with the reading as its summary, and notifies. The ledger's monitor line says
// needs you. One on its line is healthy, and one kiln could not take couldn't run, as in any monitor.
//
// A monitor is held, and starts no fix, by its last fix's outcome. A change since that fix started releases it:
//
//	last fix                                held until a change to
//	stopped, or rejected                    main's tip, any file in the monitor's directory, or what it printed
//	needs you, with a check or a gauge      its directory, or what it printed
//	needs you, with no check or gauge       main's tip, or its directory
//	unchanged, with a check or a gauge      its directory, or what it printed
//	landed, with a gauge                    its directory, or what its gauge printed
//	landed, with a check out of the gate    what its check printed
//	any other                               not held
//
// A monitor that fixes nothing is held by its last needs you, until its directory or what it printed changes. A
// healthy run releases it too. So it notifies once for each finding. kiln monitor run with a name skips that hold,
// but never starts a second fix.
//
// A healthy run drops the monitor's stopped fix the developer has not accepted, with its branch and worktree. kiln
// monitor run skips a monitor while a kiln tend is at work on it, and says so.
//
// # Fixes with a verify session
//
// A monitor.json that says "workflow": "build-and-verify" or "verify-first" splits the fix between two sessions, in
// the order Builds says:
//
//	verify verify.md's session, with the role verify, in <trees>/.verify-<name>. It writes tests from what the check
//	       printed. The proof gates them without the check.
//	fix    agent.md's session, with the role fix, on kiln/<name>. Its next message after a join names the files the
//	       verify session changed, and holds the proof's report. From there it runs as any fix's agent.
//	audit  as any fix's. Its prompt adds what the check printed, to read each test against. Its findings go to the
//	       verify session first, which answers with test files alone. kiln proves and joins its answer, then the
//	       findings go to the fix session. Once the developer accepted the fix, findings go to the fix session alone.
//
// A test file has an element of its path that a pattern of kiln.json's testFiles matches, by path.Match. So is
// each file under testHelpers, and under each acceptance path's part before {spec}. The verify session writes only
// test files, and the fix session none. So the fix never adds a test that passes, nor edits a fixture a test reads.
// Each session's hook refuses its write outside its own files, and so does each gate on its work. The proved tests
// are the commit the proof last failed on alone. Each gate of the fix, its landing's too, fails while a test file
// differs from main's with their change applied. So a landing keeps what main changed meanwhile in a test file. The
// record keeps that commit through kiln accept.
//
// A verify session that asks for the developer, fails or meets a usage limit ends the fix as Monitors says. The
// ledger marks each gate of the tests alone with proof.
//
// kiln refuses such a monitor that lacks a check, verify.md or agent.md, or holds a command. It refuses one that names
// writesTests, since its verify session alone writes the tests. It refuses one while kiln.json names no testFiles.
// kiln.json refuses an empty testFiles pattern, one path.Match cannot read, and one that holds a slash.
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
//	a command kiln runs for the project    the gate's, a monitor's check or command, a gauge, one that prints paths
//	a session                              how long it prints nothing
//
// A session that keeps printing is no hang, and the timeout never ends it. kiln kills a command past the timeout
// with its process group, and a session with each process it started. Its words name what ran past, the timeout and
// the setting:
//
//	what ran past                                          ends in
//	a gate's command, one that prints paths, a monitor's   a stopped build or fix, with those words as why
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
// worktree shares. kiln ledger takes no lock, so it never waits for a running kiln.
//
// kiln keeps each write's new ledger lines in state.json, then appends them to ledger.jsonl. A kiln killed between
// the two leaves ledger.jsonl without them. The next kiln command that writes state.json appends them. Until then,
// kiln ledger prints them from state.json.
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
//	  role          string         its session's role, such as build, verify, audit, test or fix
//	  session       string         its Claude Code session's ID
//	  at            time           when it started
//	  end           time|null      when it ended
//	  seconds       number|null    its time
//	  ended         string|null    how it ended: result, no result or usage limit
//	  cost_usd      number|null    its cost
//	  turns         number|null    its turns
//	  failed_calls  number|null    its failed calls
//	  found         number|null    findings found, by the last line of an audit run's reply; null without that line
//	  reported      number|null    findings listed, by that line; fewer than found when the audit's file caps the list
//	gate                           a run of the gate
//	  build         string         its build's ID
//	  name          string         its build's name
//	  at            time           when it started, once it held commands.lock
//	  end           time           when it ended
//	  seconds       number         its time
//	  passed        bool           whether it passed; a gate that stopped on an error did not
//	  landing       bool           whether it gated a landing, the work rebased onto main
//	  ran_main      bool|null      whether it ran the commands at main too, rather than read the checks kept there
//	  proof         bool           whether it gated the verify session's tests alone, which must fail
//	reading                        a gauge's reading of a landing
//	  build         string         the landed build's ID
//	  name          string         the landed build's name
//	  gauge         string         the gauge's name
//	  at            time           when kiln read it
//	  landed        string         the commit it read
//	  value         number|null    the reading
//	  crossed       bool           whether it crossed its line
//	finding                        a finding an audit's reply listed, or a reply kiln could not parse
//	  build         string         its build's ID
//	  name          string         its build's name
//	  fix           bool           whether its build is a monitor's fix
//	  session       string         the audit's session
//	  at            time           when the audit's run ended
//	  parsed        bool           whether kiln parsed the reply; when not, reply alone holds it
//	  rank          number|null    its number in the reply, from 1
//	  path          string|null    the file it names
//	  line          number|null    the line it names; null for a whole file
//	  category      string|null    bug, impossible by design, weak test, false comment or test-only export
//	  reaches       string|null    each of these six as the reply gives it; null when it gives none
//	  chance        string|null
//	  harm          string|null
//	  design        string|null
//	  patch         string|null
//	  proof         string|null
//	  reply         string|null    the whole reply kiln could not parse
//	monitor                        a monitor's run
//	  name          string         the monitor's name
//	  at            time           when kiln wrote it, once its check or gauge ended
//	  main          string         main's tip as it started
//	  found         string         what it found: healthy, couldn't run, held, fix, needs you or refused
//	  build         string|null    the ID of the fix it started
//	  summary       string         its check's summary, or why it couldn't run, or why kiln refused it
//	  gauge         string|null    the gauge it read in place of a check
//	  value         number|null    that gauge's reading
//	  crossed       bool           whether that reading crossed the gauge's line
//
// A sum, such as a build's cost, is null while one of its runs lacks its part. A run lacks its time and failed calls
// until it ends. A build's rounds are null while a run's start line does not say why it started. A gate's ran_main is
// null for a gate that stopped on an error first. A build's outcome is landed, stopped,
// waits on its branch, dropped or accepted. A fix's may also be needs you, session failed, usage limit, couldn't run,
// unchanged or rejected.
//
// # Gauges
//
// A gauge is a script in a monitor's directory that prints a number. Its line, in monitor.json, is atLeast or atMost,
// never both. It is named for its monitor. A monitor that holds a gauge alone watches it, and fixes nothing, as
// Monitors says:
//
//	.kiln/monitors/mutation/
//	  gauge          prints the mutation score last
//	  monitor.json   {"atLeast": 80}
//
// After each landing of a build or a fix, kiln reads each monitor's gauge at main's tip, by name. It runs each gauge in
// turn, by its path, in <trees>/.gauges at the landed commit. $KILN_BASE holds main's tip before the landing, so a
// gauge can measure one build's work alone. In a monitor's run, $KILN_BASE is git's empty tree. There git diff
// "$KILN_BASE" HEAD names every file, and git log "$KILN_BASE"..HEAD fails. The worktree holds only the commit's files.
// So a gauge that reads the changed files should list them with git diff --diff-filter=d. The reading is the last line
// of stdout, trimmed, as a decimal number. A gauge that fails, or prints no number last, reads unknown. One that exits
// 0 and prints nothing read nothing: kiln says so, and writes no reading. So kiln status keeps showing the last reading
// kiln took, with its landing. kiln says why, by the last line of its stderr, or of its stdout. Hangs says why for a
// gauge past the timeout. kiln writes each reading to its ledger with its line, and judges it against that line
// wherever it shows. A reading below atLeast, or above atMost, crossed its line. An unknown one never did. The kiln
// that landed the build prints the readings after its summary, and only then starts the next build. A gauge never
// blocks a landing, and nothing reads a landing kiln missed. The read holds commands.lock, so it waits for a gate, and
// a gate waits for it.
//
// kiln monitor run reads each gauge too, over the whole repository, as Monitors says. A gauge that read nothing there
// is healthy, with no reading, and kiln says so as after a landing. It starts no fix, and notifies nothing. So a gauge
// that measures one landing's change, such as its changed files, prints nothing in a monitor's run:
//
//	[ "$KILN_BASE" = "$(git hash-object -t tree /dev/null)" ] && exit 0
//
// # Notifications
//
// kiln notifies the developer when a spec's build or a monitor's fix needs them. A spec's build notifies once it ends
// stopped, waiting for UAT, or waiting on its branch, whichever kiln built it. A fix notifies once kiln monitor run
// ends it landed, waiting on its branch, stopped, needs you, or session failed. kiln notifies nothing of a session that
// failed the same way as the monitor's last run. So an expired sign-in notifies once per monitor. A monitor that fixes
// nothing notifies when its run ends needs you, as Monitors says. kiln monitor run notifies each landing it takes up as
// it starts, or finds on main. A landing of a fix by kiln accept notifies nothing, since the developer reads the output
// of kiln accept.
//
// kiln.json's notify names the program that receives each notification. kiln reads it at main's tip:
//
//	{"notify": ".kiln/notify"}
//
// kiln runs it with sh -c in <top>, outside the sandbox, with kiln's environment. The program's
// file is the one that checkout holds, so kiln notifies with no lock and no checkout. $KILN_NOTICE holds the notice,
// one JSON object:
//
//	{"kind": "spec", "name": "x", "state": "waiting for UAT",
//	 "summary": "4.2 min · $0.42 · 3 turns · 1 failed call\n0 of 1 round · …",
//	 "next": "read it with git show 3f9c2ab81d0e, then run kiln accept x, or …",
//	 "top": "/Users/me/scratch", "commit": "3f9c2ab81d0e…"}
//
//	kind     spec, for a spec's build, or monitor, for a monitor's fix or a watching monitor's needs you
//	name     the spec's name, or the monitor's
//	state    where it stands, as kiln status words it, such as stopped, or waiting on the developer
//	summary  the lines kiln prints below that, but the next step; a stopped spec's starts with why it stopped
//	next     what to run next, as kiln prints it after "next:"; "" when the developer has nothing to run
//	top      the top directory of the checkout kiln runs in
//	commit   the full hash of the commit that landed the build or the fix on main; absent when none did
//
// What the program prints goes to kiln's output. kiln waits 30 s for it, then kills it with its process group. A
// program that fails, runs past that, or is missing changes no build's or fix's result. kiln says why in its output.
//
// Without notify, kiln prints each notification in its output as OSC 9, which a terminal such as Ghostty shows. Its
// body is "kiln: <name> <state>". kiln prints it whether the output is a terminal or not. It prints it too when kiln
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
//	  next: run kiln tend x, to repair it in a session of your own; or kiln queue x, which builds anew
//
// A landed build shows the files its commit changed, and a build on its branch those it changed since main. That
// build shows its amendments too. The last line is the next command, never one the spec's state refuses.
package kiln

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"maps"
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
	if errors.Is(err, git.ErrNoBranch) {
		return "", errNoMain
	}

	return tip, err
}

// Repo is one repository kiln builds, opened from any of its checkouts.
type Repo struct {
	top    string
	common string
	git    gitPort
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
	top, common, err := found(ctx, dir, proc.Git.Env(env))
	if _, ran := errors.AsType[*proc.Exited](err); ran {
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

	if common == "" {
		if common, err = g.CommonDir(ctx); err != nil {
			return nil, err
		}
	}

	return newRepo(top, common, g, env)
}

// newRepo is the Repo whose checkout's top is top, and whose git directory is common. It reaches git through g, and
// starts each process with env.
func newRepo(top, common string, g gitPort, env []string) (*Repo, error) {
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

// found answers the top of the repository that holds dir, and its common git directory, from one git. Where that git
// fails, found asks for the top alone, and answers its error and no common directory. A git before 2.31 refuses
// --path-format, and Check then names the git too old.
func found(ctx context.Context, dir string, env []string) (top, common string, err error) {
	out, err := plainGit(ctx, dir, env, "rev-parse", "--show-toplevel", "--path-format=absolute", "--git-common-dir")
	if top, common, both := strings.Cut(out, "\n"); err == nil && both {
		return top, common, nil
	}

	top, err = plainGit(ctx, dir, env, "rev-parse", "--show-toplevel")
	return top, "", err
}

// cacheDir is kiln's directory under the user's cache directory.
func cacheDir() (string, error) {
	cache, err := os.UserCacheDir()
	return filepath.Join(cache, "kiln"), err
}

// HoldBuilds keeps every kiln on the machine from starting to drive a build session, until release. It refuses while
// one drives one: a kiln fire, a kiln monitor run, or a kiln accept that lands. A build's session runs kiln hook by its
// path, so the installer holds it while swapping the binary.
func HoldBuilds() (release func(), err error) {
	release, err = lockBuilds(syscall.LOCK_EX | syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, fmt.Errorf("%w: a kiln drives a build session, in this repository or another; reinstall once "+
			"it ends", errRefused)
	}

	return release, err
}

// shareBuilds takes build.lock shared, as every kiln that drives a build session does. The installer then swaps no
// binary under one. It refuses while the installer holds it.
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

// installHook installs the pre-commit hook in the repository's own directory of hooks. Where core.hooksPath is set, or
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
	main, files, err := r.git.ShowTip(ctx, mainBranch, append(readyPaths(), name.file())...)
	switch {
	case errors.Is(err, git.ErrNoBranch):
		return errNoMain
	case err != nil:
		return err
	}

	spec, err := r.queueable(ctx, main, files, name)
	if err != nil {
		return err
	}

	var drop bool
	var dropped string
	err = r.store.update(func(b *book) error {
		drop, dropped, err = b.queue(name, main, r.changed(ctx, name, spec))
		if err == nil && dropped != "" {
			b.write(entry{Kind: outcomeKind, Build: dropped, Outcome: outDropped})
		}

		return err
	})
	if err != nil {
		return err
	}

	if drop {
		if err := r.dropCheckouts(ctx, name); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "%s: %s, as main holds it at %s\n", name, queued, short(main))
	return nil
}

// queueable answers .kiln/specs/<name>.md among held, what main holds at main by path: a spec kiln queue may take.
// Its Status is Approved, and its form follows the template. Main must be ready, as ready says.
func (r *Repo) queueable(ctx context.Context, main git.Rev, held map[string][]byte, name Spec) ([]byte, error) {
	c, err := ready(ctx, main, held)
	if err != nil {
		return nil, err
	}

	spec, err := approvedSpec(held, name)
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
// its acceptance paths against the testHelpers paths, and a monitor of its name. monitors are the names under
// .kiln/monitors/.
func (c config) specRefusals(name Spec, spec []byte, monitors []string) refusals {
	refused := c.helperRefusals(name)
	if slices.Contains(monitors, string(name)) {
		refused = append(refused, refusal{fmt.Sprintf("%s holds a monitor %s, and one name is never both a "+
			"spec's and a monitor's", monitorsDir, name), "rename one"})
	}

	return append(refused, formOf(string(spec)).about(name.file()+" leaves /shape's template:")...)
}

// readyPaths are the paths ready reads: kiln.json and the build, verify and audit agents.
func readyPaths() []string {
	return []string{configFile, agentFile(buildRole), agentFile(verifyRole), agentFile(auditRole)}
}

// ready answers kiln.json among held, what main holds at main by path. It refuses while main holds no kiln.json,
// build agent, verify agent or audit agent that kiln can use. held must hold each of readyPaths that main holds.
func ready(ctx context.Context, main git.Rev, held map[string][]byte) (config, error) {
	raw, found := held[configFile]
	c, err := readConfig(raw, found)
	if err != nil {
		return config{}, fmt.Errorf("%w: %w", errRefused, err)
	}

	refused, err := c.readiness(ctx, heldFiles(main, held))
	if err != nil {
		return config{}, err
	}

	if len(refused) > 0 {
		return config{}, fmt.Errorf("%w: %w", errRefused, refused)
	}

	return c, nil
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

// testsText is the first line of body that names one of fixed, where the acceptance tests live, or "".
func testsText(body string, fixed []string) string {
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(line)
		// A part of "" is no path's, so it says nothing about where the tests live.
		names := func(part string) bool { return part != "" && startsPath(line, part) }
		if slices.ContainsFunc(fixed, names) {
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

// approvedSpec answers .kiln/specs/<name>.md among files, what main holds by path: a spec whose Status is Approved.
func approvedSpec(files map[string][]byte, name Spec) ([]byte, error) {
	if refused := name.misnamed(); refused != nil {
		return nil, fmt.Errorf("%w: %w", errRefused, refused)
	}

	spec, found := files[name.file()]
	switch {
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

// end ends the build of rec for good. It removes the build's checkouts and branches. Then one update retires rec, and
// writes e as the build's outcome. The record goes last, so a kiln interrupted or killed first leaves it for the next
// command to end. No other function takes a record out of the book.
func (r *Repo) end(ctx context.Context, rec record, e entry) error {
	if err := r.dropCheckouts(ctx, rec.Name); err != nil {
		return err
	}

	e.Kind, e.Build = outcomeKind, rec.Build
	return r.store.update(func(b *book) error {
		b.Specs = slices.DeleteFunc(b.Specs, func(s *record) bool { return s.Name == rec.Name })
		b.write(e)
		return nil
	})
}

// Reject turns away the amendments of a spec waiting on its branch, or of its stopped landing. It drops the build, its
// worktree and its branch, and kiln holds no record of the spec. Main is as it was. It marks the record rejected
// before it drops anything, so a kiln accept of name that starts meanwhile refuses. A kiln accept of name still
// running makes Reject refuse.
func (r *Repo) Reject(ctx context.Context, name Spec, out io.Writer) error {
	var rec record
	err := r.store.update(func(b *book) error {
		if err := b.may(rejectCommand, name, nil); err != nil {
			return err
		}

		s := b.find(name)
		s.Rejected, rec = true, *s
		return nil
	})
	if err != nil {
		return err
	}

	outcome := outDropped
	if rec.Monitor {
		outcome = outRejected
	}

	if err := r.end(ctx, rec, entry{Outcome: outcome}); err != nil {
		return err
	}

	fmt.Fprintf(out, "%s: rejected, and %s is gone\n", name, name.branch())
	return nil
}

// Takes answers the names command takes, for the shell to complete. For queue, accept and reject, they are specs, and
// for accept and reject, waiting fixes too. For tui, they are the specs kiln holds or its ledger holds. For "monitor
// run" and "monitor show", they are the monitors at main's tip, refused or not. kiln monitor run then says why it
// refuses one. While kiln refuses kiln.json there, Takes answers the refusal. For tend, they are the specs and
// monitors that wait on a repair. For "monitor add", they are the monitors
// kiln ships that fit the preset .kiln/generated/init.json records, as ShippedMonitors says. Each whose directory the
// checkout holds already is left out. For doctor, they are the specs the working tree holds, each by a name a spec may
// have. Other commands take none.
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
	case tendCommand:
		return r.tendNames(ctx)
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
	_, _, reads, err := r.mainMonitors(ctx)
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

	main, held, err := r.git.ShowTip(ctx, mainBranch, readyPaths()...)
	switch {
	case errors.Is(err, git.ErrNoBranch):
		return nil, errNoMain
	case err != nil:
		return nil, err
	}

	// Main that is not ready refuses every spec, so no spec need be read.
	if _, err := ready(ctx, main, held); err != nil {
		return nil, nil
	}

	files, err := r.git.Files(ctx, main, specsDir)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, f := range files {
		name, ok := strings.CutSuffix(f, ".md")
		if !ok {
			continue
		}

		spec, err := r.queueableAt(ctx, main, held, Spec(name))
		if err == nil {
			err = b.may(queueCommand, Spec(name), r.changed(ctx, Spec(name), spec))
		}

		switch {
		case errors.Is(err, errRefused):
			continue
		case err != nil:
			return nil, err
		}

		names = append(names, name)
	}

	return names, nil
}

// queueableAt reads name's spec at main, main's tip, and answers it as queueable does. held is what ready read there.
func (r *Repo) queueableAt(ctx context.Context, main git.Rev, held map[string][]byte, name Spec) ([]byte, error) {
	spec, found, err := r.git.Show(ctx, main, name.file())
	if err != nil {
		return nil, err
	}

	files := maps.Clone(held)
	if found {
		files[name.file()] = spec
	}

	return r.queueable(ctx, main, files, name)
}

// Status shows each spec kiln holds, and where it stands, but no monitor's fix. Then it shows each gauge at main's tip,
// with its latest reading. It shows each monitor main's tip holds, and where it stands, unless kiln refuses kiln.json
// there. Last, it says when .kiln/ at main's tip lacks kiln init's record, or is behind what kiln init writes.
func (r *Repo) Status(ctx context.Context, out io.Writer) error {
	main, held, err := r.git.ShowTip(ctx, mainBranch, initFile, configFile)
	switch {
	case errors.Is(err, git.ErrNoBranch):
		return errNoMain
	case err != nil:
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

	return r.initStatus(ctx, main, held, out)
}

// status shows where the spec of r stands, as kiln status does.
func (r *record) status(out io.Writer) {
	fmt.Fprintf(out, "%s: %s", r.Name, shownState(r))
	why := r.Why
	switch {
	case r.State == stopped && r.Kind == stopUnproved:
		fmt.Fprintf(out, ": %s", r.Why)
		why = ""
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

	for _, text := range []string{why, r.Amendments} {
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
// back to the fix's session, as a round, and Accept runs that session. A stopped landing waits for kiln accept
// again, or kiln reject. One with no session that conflicts with main waits for kiln reject. kiln lands a stopped
// landing again afresh, its rounds at 0, with no second fold. An accepted landing that stops fails Accept.
func (r *Repo) Accept(ctx context.Context, name Spec, out io.Writer) error {
	// Accept takes build.lock before it changes anything. While the installer holds it, Accept leaves the spec as it
	// was.
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
		err = r.acceptFix(ctx, b)
	case rec.State == amended:
		err = r.acceptAmended(ctx, b)
	default:
		err = r.acceptLanded(ctx, b)
	}

	if err != nil {
		// The failed accept lets go of the record, so kiln reject takes it at once.
		err = errors.Join(err, r.store.set(name, func(s *record) { s.Accepting = 0 }))
	}

	return err
}

// acceptLanded retires the spec of b, whose build landed, in a commit on main as it stands. It says so as that
// commit's gate starts.
func (r *Repo) acceptLanded(ctx context.Context, b *build) error {
	name := b.rec.Name
	if err := b.checkoutMain(ctx); err != nil {
		return err
	}

	retire := func(ctx context.Context, held git.Rev) (git.Rev, error) {
		// The gate runs next, and its commands may take minutes, so the developer learns why kiln is quiet.
		b.say("gates the commit that retires the spec, on main as it stands")
		return b.retire(ctx, held)
	}

	landed, back, err := b.land(ctx, "", retire, name.file(), name.amendPath())
	if err == nil && back.report != "" {
		err = fmt.Errorf("%w: the gate failed on main as it stands, so %s is not accepted:\n%s", errRefused, name,
			back.report)
	}

	// Before the landing, the spec had no branch: its build's was gone once it landed. The landing ran the gate
	// again, in a worktree of its own.
	if err != nil {
		return errors.Join(err, r.dropCheckouts(context.WithoutCancel(ctx), name))
	}

	if err := r.end(ctx, b.now(), entry{Outcome: outAccepted}); err != nil {
		return err
	}

	fmt.Fprintf(b.out, "%s: accepted, and main is at %s\n", name, short(landed))
	return nil
}

// acceptAmended folds the amendments of b's spec into the spec, on its branch, and lands it. A build with a verify
// session folds them in its clone, whose side owns the amendment file. The next join then carries the fold.
func (r *Repo) acceptAmended(ctx context.Context, b *build) error {
	name := b.rec.Name
	at := b.tree
	if b.rec.Verify.Session != "" {
		at = r.clone(name, verifyRole)
	}

	tip, err := r.git.Head(ctx, at)
	if err != nil {
		return err
	}

	if _, err := b.foldAmendments(ctx, at); err != nil {
		return errors.Join(err, r.git.Reset(ctx, at, tip))
	}

	err = r.store.set(name, func(s *record) {
		s.State, s.Accepted, s.Amendments, s.Accepting = queued, true, "", 0
		s.Code.Rounds, s.Verify.Rounds = 0, 0
	})
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
		s.State, s.Accepted, s.AcceptedAt, s.Audited, s.Accepting = queued, true, tip, tip, 0
		s.Code.Rounds, s.Verify.Rounds = 0, 0
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
	err := r.run(ctx, b.out, inLane{lane: laneOf(&b.rec), only: name, holder: acceptHolder})
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
	case held.holder == tendHolder:
		return "the kiln tend that goes on with a build"
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

// shellQuote quotes s for /bin/sh, with which Claude Code and git run a hook's command.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+,:@%=") == "" {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
