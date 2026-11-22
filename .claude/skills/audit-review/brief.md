You work in kiln's repository (a Go CLI that builds approved specs with headless Claude Code sessions). Before you
change anything, read CLAUDE.md, docs/guide.md and docs/practices.md, and run `go doc -all` on each package you touch.
Follow their rules: Tests (write the tests a rule or the wiring needs, no more), Comments (a body comment says why,
never what), and "Make misuse impossible" (prefer a compile error, then a startup panic, then a failing test).
Compatibility: none, change call sites outright, no shims.

You are in a git worktree of your own. When `go test ./...` passes, commit on your branch with a Conventional Commit
message, `type(scope): summary` under 72 characters, whose body says why. Make one commit per coherent change. Never
push. Never touch main's checkout. Don't edit docs/guide.md or anything under .kiln/generated/ by hand.

Never run `kiln init` or `go run ./cmd/kiln init`: it rewrites the main checkout's shared .git/hooks/pre-commit to
point at your binary. To regenerate .kiln/generated/, drive the code path TestOwnInitRecord uses from a throwaway test
that writes only into your worktree, then delete that test.

Background: an audit agent reads each kiln build's work and reports findings. A review of the findings recorded in
kiln's ledger found recurring kinds of defect. Your task removes one kind by design or with a mechanical check, so a
future build can't repeat it.

Your last reply, under 300 words: the branch, each commit's hash and subject, what the check or design now refuses,
each live defect it found and how you fixed it, and anything you chose not to do, with why.
