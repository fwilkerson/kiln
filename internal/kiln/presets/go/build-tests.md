Each acceptance test N of the spec has its code in a Go test in `acceptance/<name>/`, where `<name>` is the spec's
name. The test's name starts `TestAT<N>`. It drives the program through its command line or HTTP, as its user would,
and checks each clause of its Then.
