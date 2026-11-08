# 2026-11-08: three prototypes of one screen, and what each test layer caught

Each prototype draws the same fake ledger (`prototypes/ledger`) and meets the same contract (`prototypes/e2e`). Run on
an M-series Mac, Go 1.27.2, macOS, from `docs/research/tui/prototypes`.

## Measured

| Measure                                  | Bubble Tea v2 + Lip Gloss v2 | tview v0.42.0 on tcell v2.13.10 | standard library + x/term |
|------------------------------------------|------------------------------|---------------------------------|---------------------------|
| non-test lines, less blanks and comments | 165                          | 180                             | 388                       |
| own test lines, same count               | 81                           | 73                              | 61                        |
| non-standard packages built in           | 20                           | 42                              | 2                         |
| modules built in                         | 17                           | 8                               | 2                         |
| binary, plain / `-ldflags='-s -w'`       | 5.4 MB / 3.8 MB              | 4.7 MB / 3.3 MB                 | 2.7 MB / 1.8 MB           |
| e2e, 7 scenarios × 20 runs               | 0 failures                   | 0 failures                      | 0 failures                |
| chart                                    | by hand, 15 lines            | by hand, a `Box` with `Draw`    | by hand                   |
| chart library tried and dropped          | ntcharts v2.9.0              | tvxwidgets v0.14.0              | —                         |

All three, 7 scenarios each, ran 20 times in 11.2 s with `go test -count=20 ./e2e/`.

Commands:

```console
$ go test -count=20 ./e2e/
ok  	kilntui/e2e	11.169s
$ go test ./e2e -run TestScreens -count=1 -artifacts -outputdir /tmp/kilntui-screens
ok  	kilntui/e2e	0.723s
```

## Two races in the first contract, found by the prototypes

1. The contract checked a row's reverse video once, right after waiting for its text. The text was already on screen
   before the key landed, so the check read the old frame. Bubble Tea failed it in every one of 10 runs, the others
   in 6 to 9. `termtest.WaitReversed` now polls.
2. The first screen dumps after a resize were taken the same way, and showed every prototype still drawn at 100
   columns. `termtest.WaitMatch` with a pattern for a border closed at the new last column fixed it.

**Lesson:** after any input, wait for a fact the input causes, never for text that was already there.

## Faults no assertion caught, found by reading the 60 × 16 dumps

Every prototype passed every scenario, including a resize to 70 × 20. At 60 × 16, reading the screen showed:

| Prototype  | Fault                                                                                                     |
|------------|-----------------------------------------------------------------------------------------------------------|
| Bubble Tea | the lower panels lose their bottom border; detail lines are cut with no ellipsis; the detail has no frame |
| tview      | detail lines wrap mid-phrase ("context" / "25k → 426k"); "by cause" falls off the bottom with no scroll   |
| raw        | table rows overrun the right border; the footer is drawn over the last cause ("q quitacceptance/…")       |

The Bubble Tea fork had already found and fixed one such fault at 70 × 20 (a wrapped row covering the next) only by
reading a dump. The raw fork found one the same way (text over a border).

### bubbletea at 60 × 16

```text
╭──────────────────────────────────────────────────────────╮
│builds                                                    │
│#  started    spec           outcome                size …│
│1  Sun Nov 01 status-line    accepted               4    1│
│2  Mon Nov 02 usage-limits   accepted               6    3│
│3  Tue Nov 03 amendments     stopped                7    5│
│4  Wed Nov 04 amendments     dropped                7    1│
│5  Wed Nov 04 amendments     landed                 7    3│
│6  Thu Nov 05 gate-flakes    landed                 3    1│
│7  Fri Nov 06 notifications  landed                 5    2│
│8  Sat Nov 07 build-stats    waiting on its branch  6    4│
╰──────────────────────────────────────────────────────────╯
╭──────────────────────────────────╮╭──────────────────────╮
│cost per build                    ││selected              │
│     █                            ││build-stats           │
│ █ █ █ █ █ █ █ █                  ││waiting on its branch │

amendments, Tue Nov 03   size 7 · 52m0s · $22.80 · 260 turns

by role
  build  1 run · 246 turns · $21.60 · 46m0s · context 25k →
  audit  1 run ·  14 turns · $ 1.20 · 6m0s · context 18k → 9

by cause
   11  a sentence in this comment has more than 20 words
       Write internal/kiln/ledger.go
    4  refused: A variable in this command can't be checked
       cd $dir && go test
    4  refused: This Bash command contains multiple operatio
       go vet ./... && go test ./...
    1  --- FAIL: TestAT3 (0.41s)
       go test ./acceptance/kiln/ -run 'TestAT3$'

```

### tview at 60 × 16

```text
╔══════════════════════════builds══════════════════════════╗
║# started spec          outcome               size time   ║
║1 Nov 01  status-line   accepted              4    18 min ║
║2 Nov 02  usage-limits  accepted              6    31 min ║
║3 Nov 03  amendments    stopped               7    52 min ║
║4 Nov 04  amendments    dropped               7    12 min ║
║5 Nov 04  amendments    landed                7    38 min ║
║6 Nov 05  gate-flakes   landed                3    14 min ║
║7 Nov 06  notifications landed                5    26 min ║
║8 Nov 07  build-stats   waiting on its branch 6    44 min ║
╚══════════════════════════════════════════════════════════╝
┌──────────cost per build──────────┐┌───────selected───────┐
│         ███                      ││build-stats, waiting  │
│ ███ ███ ███ ███ ███ ███ ███ ███  ││on its branch         │
│ 1   2   3   4   5   6   7   8    ││started  Sat Nov 07   │
└──────────────────────────────────┘└──────────────────────┘

╔════════════════════amendments, stopped═══════════════════╗
║amendments, stopped                                       ║
║started  Tue Nov 03 09:00                                 ║
║time     52 min · 3m0s waiting · 4m0s in gates            ║
║cost     $22.80 · $3.26 per test                          ║
║turns    260                                              ║
║failed   20 calls                                         ║
║                                                          ║
║by role                                                   ║
║  build  1 run · 246 turns · $21.60 · 46 min · context    ║
║25k → 426k → 391k                                         ║
║  audit  1 run ·  14 turns · $ 1.20 · 6 min · context 18k ║
║→ 96k → 96k                                               ║
║                                                          ║
║by cause                                                  ║
╚══════════════════════════════════════════════════════════╝
```

### raw at 60 × 16

```text
┌─ builds ─────────────────────────────────────────────────┐
│ #   started     spec            outcome                 si
│ 4   Wed Nov 4   amendments      dropped
│ 5   Wed Nov 4   amendments      landed
│ 6   Thu Nov 5   gate-flakes     landed
│ 7   Fri Nov 6   notifications   landed
│ 8   Sat Nov 7   build-stats     waiting on its branch
└──────────────────────────────────────────────────────────┘
┌─ cost per build ───────────┐┌─ selected ─────────────────┐
│                            ││ build-stats · waiting on i │
│      ██                    ││ started  Sat Nov 7 09:00   │
│      ██    ██       ██     ││ size     6 acceptance test │
│   ██ ██    ██    ██ ██     ││ time     44.0m · 2.0m wait │
│██ ██ ██ ██ ██ ██ ██ ██     ││ cost     $19.90 · 230 turn │
│1  2  3  4  5  6  7  8      ││ failed   16 calls          │
└────────────────────────────┘└────────────────────────────┘

┌─ amendments · stopped ───────────────────────────────────┐
│ by role                                                  │
│   role    runs  turns     cost    time  context          │
│   build      1    246   $21.60   46.0m  25k → 426k → 391 │
│   audit      1     14    $1.20    6.0m  18k → 96k → 96k  │
│                                                          │
│ by cause                                                 │
│    11  a sentence in this comment has more than 20 words │
│        Write internal/kiln/ledger.go                     │
│     4  refused: A variable in this command can't be chec │
│        cd $dir && go test                                │
│     4  refused: This Bash command contains multiple oper │
│        go vet ./... && go test ./...                     │
│     1  --- FAIL: TestAT3 (0.41s)                         │
│ esc back · q quitacceptance/kiln/ -run 'TestAT3$'        │
└──────────────────────────────────────────────────────────┘
```

## What an agent saw in a PNG

`termtest.PNG` draws each cell with its colors in Go Mono, from `golang.org/x/image`. Reading the Bubble Tea trend PNG
at 100 × 30, this session saw the selected row in reverse video, blue bars with the selected bar in yellow, and the
panel borders. Go Mono lacks the rounded corners `╭╮╰╯`, which draw as boxes. The text dump showed none of the colors.

Running `freeze` and Microsoft's tui-test CLI to render a PNG was refused by this session's permission guard, as
code from outside. Neither was tried.

## The judge's score

A fresh agent scored the three by `docs/practices.md` "Judging two API designs", pairwise, citing each line it
counted. A is Bubble Tea, B is tview, C is raw. Measure 1 sums the hops of three reader paths: a click on a row, a
click on a bar, and a resize reaching the layout.

| # | W | Measure                           |   A |   B |   C | A–B                 | A–C    | B–C    |
|---|--:|-----------------------------------|----:|----:|----:|---------------------|--------|--------|
| 1 | 3 | hops                              |  11 |  18 |  19 | A +3                | A +3   | B +3   |
| 2 | 2 | pass-throughs                     |   0 |   2 |   0 | A +2                | 0      | C +2   |
| 3 | 2 | lines repeating another package   |  21 |   8 | 195 | B +4                | A +4   | B +4   |
| 4 | 3 | lines on rules the compiler skips |  20 |  16 |  34 | B +3                | A +3   | B +6   |
| 5 | 1 | lines                             | 165 | 180 | 388 | A +2                | A +2   | B +2   |
|   |   | **Total**                         |     |     |     | 7–7, A on measure 1 | A 12–0 | B 15–2 |

The judge's notes on the tie: if A's hand-made table does not count as repeating `lipgloss/table`, A wins 11–3. If
B's two adapter closures do not count as pass-throughs, B wins 7–5.

Its main counts under measure 4:

- A: layer IDs written as `"row:%d"` and parsed back; Lip Gloss's wrapping; `cells` kept in step with `columns`.
- B: a table row is the build's index plus one, in 7 places; every mouse handler must call `singleClick`.
- C: click arithmetic mirrors draw arithmetic in 6 places; cells hold only width-1 runes; setup and restore of the
  terminal mirror each other.

Behavior it read in the code, not scored:

- While a build is open, A's arrows still move the hidden selection. B scrolls the detail; C ignores them.
- A hits a bar only on its own cells. B hits the whole column, numbers included. C hits the whole slot.
- tview and Bubble Tea recover a panic and restore the terminal. C restores only for a panic in its main goroutine.
- Only C scrolls a long ledger today. B's table scrolls itself but has a fixed height. A has no scrolling.

It believed, without checking, that no toolkit here has a line chart. ntcharts v2 lists line charts and braille
plots; that is not tried either.
