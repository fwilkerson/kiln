Acceptance test N's code is the `node:test` test whose name starts `AT<N>`, in `tests/acceptance/<name>.test.ts`,
where `<name>` is the spec's name.

A change to a file that configures tsc or the tests, such as `tsconfig.json` or `package.json`, is a change to the
gate. Report each one that turns off a check, skips a test or stops a run early.
