---
name: slice
description: Writes a spec with the developer, from a first draft to one they approve in specs/, and revises an approved one. The developer starts it with the spec's name or idea.
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

Write the spec before you discuss anything. A draft ranks the points, and discussing one point a reply once ran 2.5
hours without reaching a design.

1. If `specs/<name>.md` exists, resume it from its Open Questions. A spec with Status Approved is being revised: keep
   its Status Approved, and edit it in place. Otherwise, copy [`template.md`](template.md) to that path.
2. Read `README.md`, `docs/practices.md` and `docs/later.md`. Run `go doc` on each package the spec touches.
3. A topic in `docs/later.md` that the spec takes up says what would show its need. Write in the spec what showed it.
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

Number them. Test N's code will be a Go test in `acceptance/<name>/` whose name starts `TestAT<N>`, and a build writes
it. Each has a Given, a When and a Then, and the developer owns their words. A build may change one only by an
amendment, in `specs/<name>.amend.md`, which the developer reads at acceptance.

- A Then names a state a test can read through the program's command line or HTTP.
- A Given, When, Then names only what the intent names: never a timeout, a length or a message's wording. A test that
  locks a detail the intent never asked for becomes an amendment later.
- Renumbering an approved test breaks the tie to its code. Add a test at the end instead.

## 4. Review once the questions are settled

When no Open Question remains, run the `zoom-out`, `comprehension` and `critic` agents on the spec, once each, in
parallel. A review of a spec with open questions reviews a spec that will still change. Run them again only when the
developer asks. A run the developer asks for goes ahead even with questions open, so say in its brief that they asked.

- Fix each finding, or contest it. To contest one, cite a file and symbol, or a line of the spec, that the agent could
  read.
- Never contest a comprehension gap. If a fresh reader could not follow the spec, the spec lacks context.
- When a finding is wrong only because of a fact no file holds, bring it to the developer.
- Fix each false claim that `zoom-out` lists, since the developer decides on those sentences.

## 5. Ask for approval

Ask the developer to approve the spec, with each contested finding beside the request. Once they approve, set its
Status to Approved, with the date, and commit it on main. Then tell them to run `kiln queue <name>`.

## Revising a spec

Revising an approved spec is editing it with this skill, from step 1. Commit the revision on main, and tell the
developer what to run next, by where `kiln status` shows the spec:

- Waiting for UAT, when UAT found a problem: `kiln queue <name>`. The new build starts from main, which holds the
  landed work, so it revises that work. kiln refuses to queue it until the spec on main has changed.
- Waiting on its branch with amendments, to change an amendment's wording: commit the developer's wording, then
  `kiln reject <name>` and `kiln queue <name>`. The rejected build never reaches main.
- Stopped, or queued: `kiln queue <name>` builds it anew from the revision.
