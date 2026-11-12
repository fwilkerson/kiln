Each acceptance test N of the spec has its code in the pytest function `test_at<N>`, in
`tests/acceptance/test_<name>.py`, where `<name>` is the spec's name. It drives the program through its command line
or HTTP, as its user would, and checks each clause of its Then.
