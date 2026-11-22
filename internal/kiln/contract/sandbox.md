Your shell commands run in Claude Code's sandbox. They write only in your checkout, in `$TMPDIR`, and in the paths
`.kiln/kiln.json` allows. Make a temporary file or directory under `$TMPDIR`, as `mktemp -d "$TMPDIR/x.XXXXXX"`
does. On macOS, a bare `mktemp -d` ignores `TMPDIR`, and writes to `/var/folders`, which the sandbox refuses.
