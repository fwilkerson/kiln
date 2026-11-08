# Go TUI libraries, and their upkeep

Read 2026-11-08 from GitHub's API, each project's releases, and the Go module proxy. Stars and open issues are as
GitHub counts them that day. Each item says what the source says; **Inferred** marks what this research infers.

## The candidates

| Library               | Latest            | Released   |  Stars | Open issues | Last push  | License    | What it is                                         |
|-----------------------|-------------------|------------|-------:|------------:|------------|------------|----------------------------------------------------|
| Bubble Tea v2         | v2.1.1            | 2026-10-28 | 45,650 |         277 | 2026-11-01 | MIT        | Elm-style loop: `Update(msg)`, `View()`            |
| Lip Gloss v2          | v2.0.6            | 2026-08-11 | 11,980 |         173 | 2026-11-01 | MIT        | styles, borders, joining blocks of text            |
| Bubbles v2            | v2.2.1            | 2026-08-24 |  9,035 |         256 | 2026-11-08 | MIT        | table, list, viewport, for Bubble Tea              |
| ntcharts v2           | v2.9.0            | 2026-10-29 |    813 |           5 | 2026-10-29 | MIT        | bar, line, sparkline, heat map for Bubble Tea      |
| tview                 | v0.42.0           | 2025-08-27 | 14,148 |         108 | 2026-08-11 | MIT        | retained widgets: table, flex, grid, pages         |
| tcell v2 / v3         | v2.13.10 / v3.6.0 | 2026-10-10 |  5,235 |          15 | 2026-11-05 | Apache-2.0 | cell screen, input, simulation screen              |
| tvxwidgets            | v0.14.0           | 2026-06-13 |    213 |           0 | 2026-11-07 | MIT        | bar chart, plot, sparkline for tview               |
| termdash              | v0.22.0           | 2026-06-09 |  3,045 |          49 | 2026-09-14 | Apache-2.0 | dashboards with built-in charts, on tcell          |
| termui v3             | v3.1.0            | 2019-07-15 | 13,582 |         106 | 2025-07-10 | MIT        | dashboards with charts; no release in seven years  |
| gocui (awesome-gocui) | v1.1.0            | 2022-01-13 |    387 |          29 | 2026-08-25 | BSD-3      | views and key bindings; lazygit keeps its own fork |
| asciigraph            | v0.10.0           | 2026-06-21 |  3,101 |           8 | 2026-10-17 | BSD-3      | a line plot as a string                            |
| golang.org/x/term     | v0.47.0           | 2026-10-09 |      — |           — | —          | BSD-3      | raw mode and terminal size; no drawing             |

## Notes per library

- **Bubble Tea v2.0.0**, 2026-02-24, moved to `charm.land/bubbletea/v2`. Its notes: a new renderer "based on the
  ncurses rendering algorithm"; mouse messages split into `MouseClickMsg`, `MouseReleaseMsg`, `MouseWheelMsg` and
  `MouseMotionMsg`; `View()` returns a `tea.View` struct whose fields set the alt screen and mouse mode, replacing
  v1's commands. Five releases from 2026-07 to 2026-10. It has no hit testing; a click is a message with a cell.
- **tview** tags rarely: v0.42.0 is its only release since 2025, and its README installs `@master`. Its author "tries
  really hard to keep this project backwards compatible", except for changes forced by tcell. Its master still
  requires tcell v2.8.1, while tcell's own releases have been v3 since 2025-12-01. Its README lists K9s, the GitHub
  CLI and podman-tui among about 90 users. It lists no chart widget; it points to tvxwidgets.
- **termdash** has built-in line charts, bar charts, sparklines and gauges, and mouse support. Its last release on
  GitHub's page is v0.20.0 of 2024; the proxy has v0.22.0 of 2026-06-09.
- **termui** has charts but no release since 2019.

**Inferred.** Two libraries are reputable enough to build on: Bubble Tea, by a funded company with frequent releases,
and tview, by one author with a strong compatibility promise and a slow cadence. termui is out on upkeep. termdash's
charts are its draw, but its users are far fewer, and Bubble Tea's charts come from ntcharts. The prototypes compare
Bubble Tea, tview, and the standard library with `golang.org/x/term` as the baseline any dependency must beat.
