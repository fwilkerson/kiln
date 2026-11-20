- Run `npm ci`, `npx tsc --noEmit` and `node --test` before you end your turn. The gate runs them too.
- Node strips the types from each `.ts` file it runs. Write only the syntax that tsc's `erasableSyntaxOnly` allows,
  and import each file by its `.ts` name.
- Give each test a name no other test has. The gate names a failed test by its name alone, not its file or suite.
