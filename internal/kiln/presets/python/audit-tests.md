Acceptance test N's code is the pytest function `test_at<N>` in `tests/acceptance/test_<name>.py`, where `<name>` is
the spec's name.

A change to a file or table that configures pytest, ruff or ty, such as `pyproject.toml`, `ruff.toml` or
`conftest.py`, is a change to the gate. Report each one that turns off a check, skips a test or stops a run early.
