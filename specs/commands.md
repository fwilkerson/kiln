# Spec 4: Commands that say what the developer does

Status: Approved, 2026-11-09

<!-- Status is Proposed or Approved, with the date it was approved. kiln deletes the spec once the developer accepts
it, and PRODUCT.md names the commit that last held it. -->

## Overview

Each command the developer runs says what it does to a spec. `kiln queue` puts a spec up to be built, `kiln accept`
takes the work, and `kiln reject` turns the amendments away. No command drops amendments the developer didn't accept
or reject. When UAT finds a problem, the developer changes the spec, and kiln builds the change through the same
workflow. The shell completes kiln's commands and the spec names each one takes. One command reinstalls kiln from
main, with its completion.

What showed the need, while slicing `specs/notify.md` on 2026-11-09:

- The developer found `kiln approve` and `kiln accept` too alike: the names don't say what each does.
- `kiln approve --again` was added in `b96c36a` to get past a missing check, not as a step of the workflow.
- An accepted landing that stops loses the accepted spec. `kiln accept x`, on a spec waiting on its branch, folds the
  amendments into `specs/x.md` on `kiln/x` only, then lands it. If the landing stops, `kiln approve x` is allowed
  with no flag, and drops `kiln/x` (`Approve` in `internal/kiln/kiln.go`). Nothing can retry the landing, since
  `kiln accept` refuses a stopped spec.

Two designers drafted the commands apart, from one brief, and agreed on all of it but the name of `queue`. The
developer picked `queue`.

## Goals

- The commands read:

  ```text
  kiln init                write a starting kiln.json, and install the pre-commit hook
  kiln queue <name>        queue a build of specs/<name>.md, as main holds it
  kiln run                 build each queued spec, one at a time, until none is queued
  kiln status              show where each spec stands
  kiln tui [<name>]        show every build, live and past, in a full-screen view; or open it on one spec
  kiln accept <name>       take the work: retire the spec, or land its amendments first
  kiln reject <name>       turn the amendments away: drop the build, and its branch
  ```

  `kiln approve` and its `--again` are gone.

- Where each command moves a spec, and where it refuses:

  ```text
  (no record) ─queue─▶ queued ─run─▶ building
                                     ├─ lands ──▶ waiting for UAT
                                     │            ├─ accept ──▶ retired
                                     │            └─ queue, once its spec on main changed ──▶ queued
                                     ├─ stops ──▶ stopped
                                     │            └─ queue ──▶ queued
                                     └─ amends ─▶ waiting on its branch with amendments
                                                  ├─ reject ──▶ (no record)
                                                  └─ accept ──▶ landing accepted amendments
                                                                ├─ lands ──▶ retired
                                                                └─ stops ──▶ landing accepted amendments, stopped
                                                                             ├─ reject ──▶ (no record)
                                                                             └─ accept ──▶ landing accepted amendments
  ```

  A spec landing accepted amendments is queued, building or stopped, as the build that lands it stands, and kiln
  keeps that it is landing accepted amendments. `kiln status` says so beside the state, as it shows a pause today.

  | State                                 | `queue`                                | `accept`                 | `reject`            |
  |---------------------------------------|----------------------------------------|--------------------------|---------------------|
  | No record                             | Queues it                              | Refuses                  | Refuses             |
  | Queued                                | Queues it again, from main's tip       | Refuses                  | Refuses             |
  | Building                              | Refuses                                | Refuses                  | Refuses             |
  | Stopped                               | Builds anew from main                  | Refuses                  | Refuses             |
  | Waiting for UAT                       | Only once its spec on main has changed | Retires it               | Refuses             |
  | Waiting on its branch with amendments | Refuses: accept or reject them first   | Folds them in, and lands | Drops build, branch |
  | Landing accepted amendments, queued   | Refuses: its landing is queued         | Refuses                  | Refuses             |
  | Landing accepted amendments, building | Refuses: its landing is building       | Refuses                  | Refuses             |
  | Landing accepted amendments, stopped  | Refuses: accept or reject them first   | Lands it again           | Drops build, branch |

  `kiln queue` also refuses a spec whose Status is not Approved, as a starter's is, and a name that is not a spec's:
  `specs/x.amend.md` is the amendments of `x`, not a spec named `x.amend`.

- When UAT finds a problem, the developer changes `specs/<name>.md` on main with `/slice`, commits it, and runs
  `kiln queue <name>`. The new build starts from main, which already holds the landed work, so it revises that work.
  A spec waiting for UAT whose spec on main has not changed since its approval can't be queued, since the same spec
  would build the same work.
- To change an amendment's wording, the developer commits their own wording to `specs/<name>.md` on main, then runs
  `kiln reject <name>` and `kiln queue <name>`.
- A landing that doesn't move main leaves `kiln/<name>` as it stood before the landing began, whether the landing
  stopped, went back to its session, or was cut off. So a landing that runs again lands the spec as accepted, and
  retires it once. Today a landing gate that fails leaves the commit that retired the spec on the branch, and the
  next landing reads a spec that is gone.
- `kiln accept` on a stopped landing starts its build afresh: its rounds at 0, and nothing kept from the stop but the
  build, its session, its audit and that it is landing accepted amendments. So `kiln status` shows no old failure
  above a landing that runs again.
- zsh completes kiln's commands, and after a command the names of the specs it takes. `kiln queue`, `kiln accept`
  and `kiln reject` offer only the specs they would not refuse. `kiln tui` offers each spec it opens on: those kiln
  holds a record of, and those its ledger holds, accepted ones included.
- zsh finds the completion as a file, `_kiln`, in `$XDG_DATA_HOME/zsh/completions`, which the developer's
  `~/.zshrc` already puts on `fpath`, beside `_lsd`. The file only asks kiln: `kiln complete` lists kiln's commands,
  and `kiln complete <command>` the spec names that command takes. So the refusal rules live once, in Go, and the
  file needn't change when a command does.
- `go run ./tools/install`, run in kiln's repository, reinstalls kiln from main's tip, with its completion. It builds
  main's tip, never the working tree, so neither an edit in progress nor a build's worktree reaches the binary. It
  installs the binary where `go install` would, in `GOBIN`, or else in the first `GOPATH`'s `bin`. It copies `_kiln`
  from the same commit into `$XDG_DATA_HOME/zsh/completions`, with `XDG_DATA_HOME` at `~/.local/share` when unset.
- The reinstall refuses while any kiln on the machine drives a build session, in this repository or another: a
  `kiln run`, or a `kiln accept` that lands. It holds that refusal from its check until the new binary is in place.
  A build's session runs `kiln hook` by its path, so a binary swapped mid-build would mix two versions of kiln in one
  build.

## Non-goals

- Completion in bash or fish. The developer's shell is zsh.
- Installing kiln from a remote. kiln never pushes, so main exists only in the local repository.
- A stopped build that goes on in its session once the developer has talked with it. `specs/talk.md` takes it up.
- A reject for a spec waiting for UAT. Its work is on main, and kiln moves main only by landing. A change to the spec,
  then `kiln queue`, is the way to change landed work.

## Open Questions

None.

## Acceptance tests

Each test runs `kiln` on a scratch repository with the scripted claude, unless it says otherwise.

### 1. Amendments wait for an answer

- **Given**: a spec waiting on its branch with amendments.
- **When**: the developer runs `kiln queue` on it.
- **Then**: kiln refuses, and `kiln status` still shows it waiting on its branch with the amendments.

### 2. Rejecting amendments drops the build

- **Given**: a spec waiting on its branch with amendments.
- **When**: the developer runs `kiln reject` on it.
- **Then**: `kiln status` no longer shows the spec, its branch is gone, and main's spec and code are as they were.

### 3. A landed spec builds again only once its spec changed

- **Given**: a spec waiting for UAT.
- **When**: the developer runs `kiln queue` on it, then commits a change to the spec and runs `kiln queue` again.
- **Then**: the first is refused. After the second, `kiln run` builds from the changed spec, starting from main with
  the first build's work on it.

### 4. An accepted landing that stops can land again

- **Given**: a spec waiting on its branch, whose landing stops once accepted.
- **When**: the developer runs `kiln queue` on it, then fixes what stopped the landing and runs `kiln accept` again.
- **Then**: the queue is refused, and `kiln status` shows the spec stopped, landing accepted amendments. After the
  second accept, main holds the spec's work, the spec as accepted, amendments folded in, is retired, and
  `PRODUCT.md` names it.

### 5. Completion offers only what a command takes

- **Given**: one approved spec waiting for UAT, one queued, one on main that kiln holds no record of, and one starter
  with Status Proposed.
- **When**: the completion asks kiln for the spec names after `kiln accept`, and after `kiln queue`.
- **Then**: `kiln accept` gets only the spec waiting for UAT. `kiln queue` gets the queued spec and the one with no
  record, and neither the spec waiting for UAT nor the starter.

### 6. The reinstall builds main

- **Given**: a clone of kiln's repository, with a commit on main and an uncommitted change in the working tree.
- **When**: the developer runs the reinstall in it.
- **Then**: the installed `kiln` was built from main's tip without the uncommitted change, and the completion
  directory holds `_kiln` as that commit has it.

## Technical Plan

The names below are kiln's own. A record is what `internal/kiln/state.go` keeps of one spec kiln holds, and the book
is every record. A record's `Approved` is main's tip when its spec was queued, and its `Accepted` says it is landing
accepted amendments.

- `internal/kiln/state.go` holds the table above, in place of `book.approve`'s rules. A spec waiting for UAT queues
  only when `specs/<name>.md` at main's tip differs from that file at the record's `Approved`. `kiln queue` reads the
  spec's Status line at main's tip, and takes only Approved.
- `internal/kiln/kiln.go`: `Approve` becomes `Queue`. A new `Reject` removes the record, the build's worktree and
  its branch, and the ledger records the build as dropped, as it records a build `Approve` drops today. `Accept`
  takes a stopped record with `Accepted` set. It rebuilds the record whole, keeping only what the Goals name, and
  lands it in place as it lands an accepted spec today, with no second fold.
- The landing (`build.land`, in `internal/kiln/build.go`): before it moves `kiln/<name>`, the record keeps the
  commit the branch stood at. When the landing doesn't move main, the branch goes back to that commit, before the
  build goes back to its session or stops. A `kiln run` that finds a kept commit, left by a landing cut off, puts
  the branch back there before it goes on.
- `Status` adds ", landing accepted amendments" to a record with `Accepted` set, in any state.
- `cmd/kiln/main.go`: the usage above. `kiln complete` prints kiln's commands, one per line. `kiln complete <command>`
  prints the spec names that command takes, by the same rules the command refuses by. For `tui`, it prints the names
  `kiln tui` lists. `complete` joins `hook` and `precommit` as a command kiln runs for itself.
- `cmd/kiln/_kiln`: the zsh completion function. It completes commands from `kiln complete`, and names from
  `kiln complete <command>`.
- The build lock: every kiln that drives a build session, `run` and so `accept`'s landing too, holds a shared lock
  on one file in kiln's directory under the user's cache directory, where kiln keeps its worktrees. `store.lock`
  gains a shared mode for it. The reinstall takes that lock exclusively, without waiting, and holds it from its
  check until the new binary is in place. `internal/proc` doesn't change: its process table holds no command line.
- `tools/install/`: the reinstall. It makes a temporary worktree at main's tip, runs `go install ./cmd/kiln` there,
  copies `cmd/kiln/_kiln` from it, and removes the worktree.
- Every other mention of `kiln approve` and `--again` changes with the command: `README.md`, `docs/practices.md`
  (which shows `kiln approve --again` as kiln's flag style), the `/slice` skill in `guidance/skills/slice/SKILL.md`,
  the refusal in `state.go`, and the acceptance scripts of accepted specs.
- Two accepted acceptance tests change their code, though not their Given, When, Then:
    - Spec 1's test 7, "Revising a spec builds it again", edits a spec waiting on its branch and runs
      `kiln approve --again`. It becomes `kiln reject`, then `kiln queue`.
    - Spec 3's test 5, "The trend on the home screen", builds one spec 12 times: one `kiln approve`, then 11
      `kiln approve --again`. Each of those 11 now commits a change to the spec first, since an unchanged landed spec
      can't be queued.
- `acceptance/commands/`: tests 1 to 4 follow `acceptance/kiln/testdata/at5.txtar`, `at1.txtar` and `at9.txtar`.
  Test 4's landing stops because main's checkout has an uncommitted change to a file the landing changes, which the
  developer then commits away. Test 5 runs `kiln complete`, which the completion function calls; no session can open
  the pseudo-terminal that zsh's own completion would need. Test 6 clones the commit under test into the script's
  directory as main, since main in kiln's gate doesn't hold `tools/install` yet. Its `GOBIN` and `XDG_DATA_HOME` are
  in the script's directory, and its `HOME` is the script's own, so the build lock of the `kiln run` that gates it
  is not its lock.

## Alternatives Considered

### `kiln build <name>` in place of `kiln queue <name>`

- **Pros**: It says the spec may now be worked on.
- **Cons**: It reads as building now, but `kiln run` builds. `docs/practices.md` holds one word to one meaning.
- **Reason for Rejection**: The developer picked `queue`, the word `kiln status` already shows for the state.

### Keep a flag to rebuild a landed spec as it stands

- **Pros**: A rebuild needs no spec change.
- **Cons**: The same spec builds the same work, and a problem UAT finds is a Then the spec lacks, since an acceptance
  test's code tests what its Then says.
- **Reason for Rejection**: That flag is what `--again` became.

### `kiln completion zsh`, evaluated in `~/.zshrc`

- **Pros**: The script always matches the binary, as `gh`'s does.
- **Cons**: It starts kiln at each shell start, and needs a line in `~/.zshrc` after `compinit`.
- **Reason for Rejection**: The script only calls `kiln complete`, so it has nothing to keep fresh. A blind agent
  weighed both, and picked the file for the same reason.

### `make install`, or `kiln install`

- **Pros**: `make install` is a common name. `kiln install` needs no checkout.
- **Cons**: The repo has no `Makefile`, and macOS ships no `flock` for a shell to check a running build. kiln builds
  other repositories, where `kiln install` would mean nothing.
- **Reason for Rejection**: `tools/style` is already a Go program in the repo, and a Go program can read the process
  table through `internal/proc`.

### A third verb to reword amendments

- **Pros**: One command in place of a commit, a reject and a queue.
- **Cons**: The branch's work meets the session's wording, and a reworded spec needs a new build anyway.
- **Reason for Rejection**: Committing the wording, then reject and queue, says each step plainly.
