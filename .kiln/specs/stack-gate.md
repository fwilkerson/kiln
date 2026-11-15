# Spec N: the verify session gates the stack in its own turn

Status: Proposed

<!-- A starter, drafted on 2026-11-14 from odh's trial, where the build of hand-over stopped. Run /shape stack-gate to
take it up: the skill resumes it from its Open Questions, then writes the acceptance tests and the Technical Plan. -->

## Overview

In a split build, the verify session writes the acceptance tests and never runs them on the build's code. Only kiln's
gate does, after the session's turn. When that gate fails, the verify session reads the report and rules alone: the
test is at fault, or the code is. So it rules by reading, and each test it changes costs a round to try.

```text
today    verify changes a test ──▶ ends its turn ──▶ kiln stacks and gates ──▶ one round spent, pass or fail
with     verify changes a test ──▶ runs kiln gate ──▶ reads the report ──▶ changes it again, in the same turn
```

What showed the need: odh's trial on 2026-11-14. Four checks in the verify session's tests were inverted, so each
failed when the code was right. One run with one line changed shows it. The verify session blamed the code in two
rounds of three, the build session changed working code, and the build stopped.

On a failed stack, the verify session gets one command. It stacks the session's test files, as its checkout holds
them, on the build's code. It runs the gate's commands there, outside the sandbox, and prints the gate's report. The
session still never reads the build's code.

The mechanism was tried on 2026-11-14, in a sandboxed session. A command named in the settings'
`sandbox.excludedCommands`, with an allow rule, ran outside the sandbox with no prompt.

## Goals

- After a failed stack, the verify session runs its changed tests on the build's code, in its own turn.
- It reads only what the gate's report holds, as it does today.

## Non-goals

- A way for either session to read the other's work.
- A change to who rules on a failing stack, or to the bugs line.

## Open Questions

<!-- Keep each question here, across sessions, until it is answered. Then move the answer to where it belongs. -->

1. What is the command named? Lean: `kiln gate`, since it runs the gate, and the session's prompt already says "the
   gate".
2. Does a run the session asks for count as a round? Lean: no. A round is a failing gate that goes back to a session,
   and this run goes back to no one.
3. Does the build session get the command too, to run the verify session's tests on its code without reading them?
   Lean: no. In the trial, the build session reproduced the flow by hand and found its code right. The session that
   rules is the one that lacked a run.
