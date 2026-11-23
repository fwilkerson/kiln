# The verify session can read the audit's transcripts, and the build session the verify session's stream

Found 2026-11-23, by the audit of the workflows change, commit 839db82.

## What happened

`Repo.blinds` in `internal/kiln/checkouts.go` denies a session the transcripts of its rival's checkout alone. It
denies the repository's git directory to the verify session's shell, and not to the build session's, since git
needs it there. Two reads stay open:

- The verify session may read the audit's transcripts, in the folder Claude Code names for `<trees>/.audit-<name>`.
  The audit reads the build's work, so its transcripts hold that code. The folder is there by the time the verify
  session answers the audit's findings.
- The build session's shell may read `.git/kiln/sessions/<id>/`, where kiln keeps each session's stream, the
  verify session's too. Only the Read tool is denied the git directory. Side by side, that stream grows while the
  build session's first run goes on, before any test is proved.

## What should happen

The verify session reads no transcript that holds the build's work, the audit's included. The build session reads
no stream and no transcript of the verify session's. git still works in the build session's worktree.

## How to see it

Build a spec under `"workflow": "build-and-verify"` whose audit sends findings, and read the settings kiln gives
each session. The verify session's second run denies the folder under `projects/` named for the build's worktree,
and names none for `.audit-<name>`. The build session's `denyRead` holds `<trees>` and the verify session's
transcripts, and no path under `.git/kiln`. The settings show both gaps. No scenario has yet made a session read
through one.
