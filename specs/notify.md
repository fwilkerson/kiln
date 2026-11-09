# Spec N: Tell the developer when a spec waits on them

Status: Proposed

<!-- A starter, taken from docs/later.md on 2026-11-08. Run /slice notify to take it up: the skill resumes it from its
Open Questions, then writes the acceptance tests and the Technical Plan. -->

## Overview

The developer approves a spec and walks away. When the spec needs them again, a macOS notification says which spec
and why, and a click on it brings them to where they act on it.

What showed the need: the developer wants to try Ghostty's notifications.

## Goals

- `kiln run` notifies when a spec stops, waits for UAT, or waits on its branch with amendments. These are the three
  states in `internal/kiln/state.go` that wait on the developer.
- The notification names the spec and its state.

## Non-goals

- A notification for a state that needs nothing from the developer, such as queued or building.

## Open Questions

1. **Which way does kiln notify?** None of these is tried; each line is believed.

   | Option                                          | Needs an install | A click opens                 | An acceptance test reads it      |
   |-------------------------------------------------|------------------|-------------------------------|----------------------------------|
   | a) `kiln run` prints OSC 9 or OSC 777 (Ghostty) | No               | The terminal that ran it      | In `kiln run`'s output           |
   | b) `terminal-notifier`                          | Yes, Homebrew    | A command, such as `kiln tui` | Only through a scripted stand-in |
   | c) `osascript`                                  | No               | Nothing                       | Only through a scripted stand-in |

   Lean a), as the developer wants to try Ghostty first, and it is the one a test reads through the command line. The
   slice should try OSC 9 in Ghostty before the spec is approved.

2. **What should a click open?** With a), it brings forward the terminal that runs `kiln run`, where the developer
   runs `kiln status` or `kiln tui <name>`. Opening `kiln tui <name>` itself needs b). Lean: a)'s terminal is enough
   for a first spec.
