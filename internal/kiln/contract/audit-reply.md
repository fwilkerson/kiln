### Your reply

kiln reads your reply in the form below, whatever the text above says of it. List each finding you report this way,
ranked, with a blank line after each:

```text
1. path:line  kind
reaches    the command or call, the state, and what goes wrong,
           indented where it runs on to the next line
chance     what must happen in the program's wiring for the path to run, and how often that is
harm       what a user or the data ends with, and what heals it
design     the change that makes it impossible to build, or "none"
patch      the smallest fix, only when design is "none"
proof      the test or check that fails today, or "none" when no test can fail today
```

- Number the findings from 1.
- Write the place as `path:line`, or `path` alone for a whole file.
- kind is one of `bug`, `impossible by design`, `weak test`, `false comment` and `test-only export`.
- Start each field at the start of a line, with its name. Its text may run on to the next lines, each indented.
- Leave out a field that does not apply.

End your reply with one line: `read N directories, found M, reported K`. N counts the directories you read. M counts
every finding you found, and K the findings you listed. K is less than M only when your instructions cap the list.

When M is not 0, kiln sends your whole reply to the session whose work you read.
