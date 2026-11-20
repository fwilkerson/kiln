Acceptance test N's code is a `node:test` test whose name starts with the spec's name and `AT<N>`, such as
`login AT1`, in the acceptance tests' path kiln names. The gate tells tests apart by name alone, so the spec's name
keeps two specs' tests apart. It drives the program through its command line or HTTP, as its user would, and checks
each clause of its Then.
