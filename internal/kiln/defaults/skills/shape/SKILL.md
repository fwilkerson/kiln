---
# This skill writes the specs kiln builds. kiln init wrote it, and it is the project's now: change any line to
# make it your own. kiln refuses to queue a spec that leaves the form of kiln's own template.md, so a change to
# the form of this one does not reach kiln.
name: shape
description: Writes a spec with the developer, from a first draft to one they approve in .kiln/specs/, and revises an approved one. The developer starts it with the spec's name or idea.
argument-hint: <spec name or idea>
disable-model-invocation: true
---

# Write a spec

A spec is what the developer wants built, and the acceptance tests that show it was. This skill takes a spec from an
idea to one the developer approves, and queues it. Building it comes after: `kiln fire` builds each queued spec, and
a build never asks the developer anything.

The developer's turns go to goals, taste, plans only they hold, and choices that are hard to undo. Settle the rest
yourself, wherever a file, the code or a measurement settles it.

## 1. Draft before the first question

Write the spec before you ask anything. A draft shows which questions are real, and what each answer changes.

1. If `.kiln/specs/<name>.md` exists, resume it from its Open Questions. A spec with Status Approved is being
   revised: keep its Status Approved, and edit it in place. Otherwise, copy [`template.md`](template.md) to that path.
2. Read `README.md`, the repository's practices, and its record of what was deferred, if it writes them down. Read
   the doc of each package or module the spec touches.
3. A deferred topic that the spec takes up says what would show its need. Write in the spec what showed it.
4. Fill every section you can, under your lean for each Open Question. Where the spec could be cut smaller or grown,
   write the cut as an Open Question.

Then tell the developer in a few lines what the spec builds. Name each Open Question in one line, with no option
and no lean, so the developer sees what is coming. Ask the first question in that same reply, as step 2 says.

## 2. Settle the Open Questions, one at a time

A shape session exists to get these questions answered. Each one blocks the spec, since the work cannot go on
until the developer picks. So an Open Question is a choice to ask, never a point to discuss or an agenda to list.
Until no Open Question remains, each reply to the developer ends with one question. Ask it as the session asks any
choice that blocks its work, with these parts:

- what the developer is deciding, in one line, and how many questions come after it
- each option under a letter, with what the spec builds if they pick it
- your lean, and the fact that decides it

An Open Question holds what only the developer can decide. Rank the questions in the spec, and ask them in that
order. A question comes before each question that its answer could change or drop. Among the rest, the question
whose answer changes more of the spec comes first. The developer may answer with a letter. They may also answer a
question you have named but not yet asked.

When the developer answers:

- Move the answer into the spec, and drop its question. Change each test, and each line of the plan, that rested on
  another option.
- Read each question that remains against the answer. Drop one that the answer settled, and add one that it opened.
- Ask the next question in the same reply.

When the developer replies with a question or an objection, they have not chosen. Check their point against the code
and the docs, and answer it. Then ask the same question again, with the options as their point leaves them.

Between questions, do the work that waits on no answer, such as research, a check, or a section an answer freed.

- Check that a cost or a risk applies to the repository today before you raise it.
- The size of a change is never a reason to cut a piece or keep one.
- Prefer a fix the developer already plans over a workaround that avoids it. Ask when you cannot tell whether one is
  planned.

## 3. Write the acceptance tests

Number them. A build writes test N's code where `.kiln/kiln.json`'s acceptance says, as the verify agent's file,
`.kiln/agents/verify.md`, says. Each has a Given, a When and a Then, and the developer owns their words. A build may
change one only by an amendment, in `.kiln/specs/<name>.amend.md`, which the developer reads at acceptance.

- A Then names a state a test can read through the program, as its user reaches it.
- With kiln.json's "testSession": true, a verify session writes the tests from the spec alone, and never sees the
  code. So the Technical Plan names each output form a Then reads, such as a line a command prints.
- A Given, When, Then names only what the intent names: never a timeout, a length or a message's wording. A test that
  locks a detail the intent never asked for becomes an amendment later.
- Renumbering an approved test breaks the tie to its code. Add a test at the end instead.

## 4. Review once the questions are settled

The reviewers are the agents in `.kiln/reviewers/`, one file each. `kiln init` links each into `.claude/agents/`,
where Claude Code finds it. If one will not start, ask the developer to run `kiln init` and restart the session.
When no Open Question remains, run each reviewer on the spec, once, in parallel, by the `name` in its frontmatter.
A review of a spec with open questions reviews a spec that will still change. A run the developer asks for goes ahead
even with questions open, so say in its brief that they asked.

A reviewer's frontmatter says how its findings are handled, as `.kiln/reviewers/zoom-out.md` and `critic.md` show:

- `uncontested` lists the kinds of finding, by the labels of its reply, that you fix and never contest. `all` means
  every finding it reports. The comment above the key says why.
- `rerun: true` runs the reviewer once more after the fixes, as the end of this step says.

Handle each finding by these rules:

- Fix each finding, or contest it. To contest one, cite a file and symbol, or a line of the spec, that the reviewer
  could read.
- When a finding is wrong only because of a fact no file holds, bring it to the developer.
- A finding that two reviewers report is not yours to contest. Fix it, or bring it to the developer.

The fixes change the spec the reviewers read. Wait until each finding is fixed, contested or answered by the
developer. Then run once more each reviewer with `rerun: true`, if the fixes changed what a Goal, an acceptance test
or the Technical Plan requires. Name the changed sections in its brief. A fix that only names a file the plan breaks
does not count as such a change. Fix or contest what a second run finds, and stop there: a second run's fixes never
start a third. Beyond that, run a reviewer again only when the developer asks.

## 5. Ask for approval

First run `kiln doctor <name>`, which prints each problem kiln queue would refuse in the spec, and in the project.
Fix each problem in the spec. Bring each other problem to the developer beside the request.

Ask the developer to approve the spec. Put beside the request each contested finding, and each finding two reviewers
reported that you brought to the developer. Put each risk the spec accepts there too, since only the developer
accepts one. A risk is a way the work can go wrong that the spec names and leaves open; a Non-goal is not one. For
each, give the rule that would make it impossible in code and what that rule costs, or say that you found none.

Once they approve, set its Status to Approved, with the date, and commit it on main. Then run `kiln queue <name>`:
their approval covers the queueing, so queue without asking again. Show the developer the line it prints. If it
refuses, it prints why: fix that in the spec, or bring it to the developer. Tell them `kiln fire` builds what is
queued.

## Revising a spec

Revising an approved spec is editing it with this skill, from step 1. Once the developer approves the revision, commit
it on main, and queue it, by where `kiln status` shows the spec:

- Waiting for UAT, when UAT found a problem: run `kiln queue <name>`. The new build starts from main, which holds the
  landed work, so it revises that work. kiln refuses to queue it until the spec on main has changed.
- Waiting on its branch with amendments, to change an amendment's wording: commit the developer's wording.
  `kiln reject <name>` drops a build that passed, so ask the developer before you run it. Then run
  `kiln queue <name>`. The rejected build never reaches main.
- Stopped, or queued: run `kiln queue <name>`, which builds it anew from the revision.
