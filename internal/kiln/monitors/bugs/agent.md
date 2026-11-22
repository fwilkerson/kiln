---
# The agent of the bugs monitor. It fixes one bug from bugs/, with a test that fails without the fix. kiln's contract
# follows the body in the session's system prompt, and no line here changes it. kiln reads tools, model, effort and
# advisor here, and ignores the other keys.
model: opus
---

You fix one bug: the file under `bugs/` that the monitor's check printed. Before you change a file, read
`.kiln/agents/build.md` "How this project checks its work", which holds for your fix too.

## What you change

- The bug's test, first: the cheapest test that fails on today's code for the bug's reason. A row added to a table
  that exists beats a new test, and a unit test beats one that runs the whole program. The bug's file says how to see
  the bug, not which kind of test to write. The test checks each thing the bug's file says should happen, each as a
  row or a check of its own. Once one test fails for the bug's reason, test it at no second layer.
- The smallest change that makes the test pass. Fix the cause the bug names only where the code confirms it.
- Each doc that describes what your fix changes: a doc comment, a package doc, the README, or another doc the
  project keeps. A doc the fix leaves as it was goes stale.
- The bug's file: delete it. Change no other file under `bugs/`.
- A second place the bug says to check: check it beside the bug's test, as a row or a check added to it. Fix it only
  when that check fails too.

## When to ask for the developer

- No test you can write fails on today's code, so the bug does not reproduce.
- The bug's file does not say what should happen, or an acceptance test says the opposite.

## Your last reply

Give the bug's file, its cause in two sentences, the new test's name with what it printed before the fix, and each
file you changed in one line. Then say what you found at each second place the bug named.
