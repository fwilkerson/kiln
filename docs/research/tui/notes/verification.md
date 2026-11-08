# How others check a TUI, and what an agent can read

Sources read 2026-11-08. Each item says what the source says, then what this research infers, marked **Inferred**.

## Four layers, as the ecosystem names them

The SpecStory guide on testing terminal UIs names the layers most projects use
([source](https://specstory.com/learning/cli-and-web/testing-terminal-uis)):

| Layer              | What runs                                                   | Examples                                                                  |
|--------------------|-------------------------------------------------------------|---------------------------------------------------------------------------|
| In-process backend | the app's model, drawn to an in-memory screen               | Bubble Tea's teatest, tcell's simulation screen, Ratatui's `TestBackend`  |
| Multiplexer        | the real binary in a tmux pane; `send-keys`, `capture-pane` | `fcoury/tmux-tui-testing-skill`, `nickgnd/tmux-mcp`                       |
| PTY plus emulator  | the real binary in a pseudo-terminal, read by an emulator   | Microsoft's tui-test, `onesuper/tui-use`, node-pty with `@xterm/headless` |
| Recording          | a scripted session rendered to text, GIF or PNG             | charmbracelet/vhs, charmbracelet/freeze                                   |

The guide warns that an agent's tests can send keys straight to the app's state and pass. It recommends driving the
real built program.

## Tools, with what each says about itself

- **Microsoft tui-test 0.1.1**, 2026-10-23 ([repo](https://github.com/microsoft/tui-test)). It "controls, inspects,
  tests, and records real shell sessions and full-screen terminal apps". Backends: Alacritty (default), Ghostty, Rio,
  xterm.js. It has a CLI and bindings for Rust, Python, JavaScript and Go
  (`github.com/microsoft/tui-test/bindings/go@v0.1.1`). Locators such as `GetByText` retry until they pass. It clicks,
  drags and scrolls the mouse, and can click on text. It saves screenshots as text, SVG or PNG. Its README says it
  "works for AI agents that need structured access to terminal state", and ships an agent skill. The Go binding embeds
  a prebuilt Rust engine and loads it through purego at first use. The API changed wholly between the 2024 npm
  release candidates and 0.1.
- **tui-use** ([repo](https://github.com/onesuper/tui-use)). "Like BrowserUse, but for the terminal." A Node.js CLI
  with a daemon, reading the screen through headless xterm. Its `wait` returns once output has been quiet for 100 ms.
  Each snapshot lists reverse-video spans as `highlights`, which shows the selected item. It argues that "`tmux
  send-keys` has no way to signal when a program is done responding". No mouse input.
- **tmux** with `send-keys` and `capture-pane -p` is the most written-up pattern for Claude Code
  ([crystl.dev](https://crystl.dev/guides/tmux-for-ai-agents/)). The same guide calls screen scraping "brittle by
  construction". `capture-pane` drops colors unless given `-e`. tmux is not installed on this machine.
- **teatest v2** (`github.com/charmbracelet/x/exp/teatest/v2`, a pseudo-version, no tag). Its pattern is
  `NewTestModel(t, m, WithInitialTermSize(80, 24))`, `Send`, `FinalOutput`, and a golden file. A 2026-10 pull request
  on `momiji-rs/termshot-action` found that `WaitFor` on `Output()` consumes the output, that an alt-screen program
  clears it on quit, and that goldens differ between macOS and Linux unless `TERM` and the color profile are fixed
  ([PR #5](https://github.com/momiji-rs/termshot-action/pull/5)).
- **go-internal's testscript**, already in kiln's `go.mod` at v1.16.0, has `ttyin` and `ttyout`. Read at that version:
  `ttyin` writes a whole file to a pseudo-terminal at once, and `ttyout` matches the raw bytes the program wrote. It
  has no emulator, so a test would match escape sequences, not a screen, and could not wait for a frame before typing.
- **charmbracelet/x/vt v0.1.0**, 2026-10-12. A terminal emulator in Go. A program's output is written into it, and
  `SendKey`, `SendMouse` and `SendText` encode input in the modes the program enabled. It answers the program's
  queries about the terminal. Its cells carry colors and attributes. It depends on `charmbracelet/ultraviolet`, which
  has no tagged release.

**Inferred.** kiln's acceptance tests already drive the real binary from the command line. A PTY-plus-emulator layer is
the same idea for a full-screen program, so it is the layer an acceptance test belongs in. The prototypes' shared
`termtest` package is that layer in 211 lines of Go, on `creack/pty` and `charmbracelet/x/vt`, with a PNG
writer on `golang.org/x/image`.

## What an agent can see

- An agent reads text. Every layer above can hand it the screen as text, one line per row.
- Text loses which row is selected, which panel has focus, and color. tui-use answers that with `highlights`.
  `termtest.Reversed` answers it for one cell.
- An agent can also read a PNG. tui-test writes one. VHS writes PNG and GIF frames, but needs `ttyd` and `ffmpeg`,
  neither installed here.
- tui-test's CLI and Go binding were first refused by this session's permission guard, then tried once the developer
  approved. See [the tui-test run](../runs/2026-11-08-tui-test.md).
