# A landing's session that never began is resumed with no instructions

Found 2026-11-22, by the agent that made `claude.Run` start any session that never began.

## What happened

A session's first message now goes in `claude.Launch.First`, which `claude.Run` sends when the session never
began. A build session and a verify session set it. A monitor's fix session does not: `fixLaunch` in
`internal/kiln/fix.go` sets only `Prompt`. When kiln dies after it records a landing's conflict session but before
that session prints its first line, the next run reaches `landFix`, which tells the session "kiln stopped while you
worked. Go on where you stopped." The session never saw the conflict, so it has nothing to go on with. kiln keeps
no copy of the conflict report that first message held.

## What should happen

A fix's session that never began starts with the message it would have got: the files that conflict with main,
and what to do with them. kiln either keeps that message with the record, or works it out again from the
worktree, as the next run finds it.

## How to see it

Kill kiln between the record of a landing's conflict session and the session's first line, as
`TestKilledAtEachStep` can with `KILN_CRASH_AT` in a flow whose accepted fix conflicts with main. Run
`kiln accept` again. The session's first prompt is "kiln stopped while you worked".
