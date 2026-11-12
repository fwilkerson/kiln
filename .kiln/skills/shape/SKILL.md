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
idea to one the developer approves. Building it comes after: the developer runs `kiln queue <name>`, then
`kiln run`, and a build never asks the developer anything.

The developer's turns go to goals, taste, plans only they hold, and choices that are hard to undo. Settle the rest
yourself, wherever a file, the code or a measurement settles it.

## 1. Draft before the first point

Write the spec before you discuss anything. A draft ranks the points, so the developer answers them together, in any
order, rather than one point a reply.

1. If `.kiln/specs/<name>.md` exists, resume it from its Open Questions. A spec with Status Approved is being
   revised: keep its Status Approved, and edit it in place. Otherwise, copy [`template.md`](template.md) to that path.
2. Read `README.md`, the repository's practices, and its record of what was deferred, if it writes them down. Run
   `go doc` on each package the spec touches.
3. A deferred topic that the spec takes up says what would show its need. Write in the spec what showed it.
4. Fill every section you can. Where the spec could be cut smaller or grown, write the cut as an Open Question.

Then show the developer the Overview and the Open Questions.

## 2. Settle the Open Questions

An Open Question holds what only the developer can decide. Rank the questions, and give each your lean and the fact
that decides it. The developer answers in any order. Move each answer into the spec, and drop its question.

- Check that a cost or a risk applies to the repository today before you raise it.
- The size of a change is never a reason to cut a piece or keep one.
- Prefer a fix the developer already plans over a workaround that avoids it. Ask when you cannot tell whether one is
  planned.

## 3. Write the acceptance tests

Number them. A build writes test N's code where `.kiln/kiln.json`'s acceptance says, as the verify agent's file,
`.kiln/agents/verify.md`, says. Each has a Given, a When and a Then, and the developer owns their words. A build may
change one only by an amendment, in `.kiln/specs/<name>.amend.md`, which the developer reads at acceptance.

- A Then names a state a test can read through the program, as its user reaches it.
- In the split workflow, a verify session writes the tests from the spec alone, and never sees the code. So the
  Technical Plan names each output form a Then reads, such as a line a command prints.
- A Given, When, Then names only what the intent names: never a timeout, a length or a message's wording. A test that
  locks a detail the intent never asked for becomes an amendment later.
- Renumbering an approved test breaks the tie to its code. Add a test at the end instead.

## 4. Review once the questions are settled

When no Open Question remains, run the `zoom-out`, `comprehension` and `critic` agents on the spec, once each, in
parallel. A review of a spec with open questions reviews a spec that will still change. A run the developer asks for
goes ahead even with questions open, so say in its brief that they asked.

- Fix each finding, or contest it. To contest one, cite a file and symbol, or a line of the spec, that the agent could
  read.
- Never contest a comprehension gap. If a fresh reader could not follow the spec, the spec lacks context.
- When a finding is wrong only because of a fact no file holds, bring it to the developer.
- Fix each false claim that `zoom-out` lists, since the developer decides on those sentences. Name in the Technical
  Plan each test, file or doc that it says the plan breaks, or contest one as above.
- A finding that two agents report is not yours to contest. Fix it, or bring it to the developer.

The fixes change the spec the agents read. Wait until each finding is fixed, contested or answered by the developer.
Then run `critic` once more if the fixes changed what a Goal, an acceptance test or the Technical Plan requires, and
name the changed sections in its brief. Naming the files the plan breaks does not count as such a change. Fix or
contest what it finds, and stop there: a second run's fixes never start a third. Beyond that, run an agent again
only when the developer asks.

## 5. Ask for approval

Ask the developer to approve the spec. Put beside the request each contested finding, and each finding two agents
reported that you brought to the developer. Put each risk the spec accepts there too, since only the developer
accepts one. A risk is a way the work can go wrong that the spec names and leaves open; a Non-goal is not one. For
each, give the rule that would make it impossible in code and what that rule costs, or say that you found none.

Once they approve, set its Status to Approved, with the date, and commit it on main. Then tell them to run
`kiln queue <name>`.

## Revising a spec

Revising an approved spec is editing it with this skill, from step 1. Commit the revision on main, and tell the
developer what to run next, by where `kiln status` shows the spec:

- Waiting for UAT, when UAT found a problem: `kiln queue <name>`. The new build starts from main, which holds the
  landed work, so it revises that work. kiln refuses to queue it until the spec on main has changed.
- Waiting on its branch with amendments, to change an amendment's wording: commit the developer's wording, then
  `kiln reject <name>` and `kiln queue <name>`. The rejected build never reaches main.
- Stopped, or queued: `kiln queue <name>` builds it anew from the revision.
