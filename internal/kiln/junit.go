package kiln

import (
	"encoding/xml"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
)

// junitSuite is a <testsuites> or a <testsuite> of JUnit XML. Either holds suites and cases, to any depth.
type junitSuite struct {
	Suites []junitSuite `xml:"testsuites"`
	Inner  []junitSuite `xml:"testsuite"`
	Cases  []junitCase  `xml:"testcase"`
}

// junitCase is one <testcase>. A case with neither a failure nor an error passed, or was skipped.
type junitCase struct {
	Classname string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Failure   *struct{}     `xml:"failure"`
	Error     *junitMessage `xml:"error"`
}

// junitMessage is an <error>, which says what went wrong in its message.
type junitMessage struct {
	Message string `xml:"message,attr"`
}

// testErrors are how pytest's messages start for an error in a test's setup or teardown. Such an error is that test's
// failure. Any other error, such as a module that will not import, breaks the run.
var testErrors = []string{"failed on setup with", "failed on teardown with"}

// readJUnit reads the JUnit XML at path for the tests that failed and the tests that ran. It names each once, sorted.
// broken is true when the file will not parse, or holds an error that is no test's failure. A missing file names no
// test.
func readJUnit(path string) (failed, ran []string, broken bool) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, false
	}

	var root junitSuite
	if err == nil {
		err = xml.Unmarshal(raw, &root)
	}

	if err != nil {
		return nil, nil, true
	}

	failed, ran, broken = root.read()
	slices.Sort(failed)
	slices.Sort(ran)
	return slices.Compact(failed), slices.Compact(ran), broken
}

// read answers what s and every suite inside it hold, unsorted.
func (s junitSuite) read() (failed, ran []string, broken bool) {
	for _, inner := range append(slices.Clone(s.Suites), s.Inner...) {
		f, r, b := inner.read()
		failed, ran, broken = append(failed, f...), append(ran, r...), broken || b
	}

	for _, c := range s.Cases {
		name := c.name()
		ran = append(ran, name)
		switch {
		case c.Failure != nil, c.Error != nil && c.Error.inTest():
			failed = append(failed, name)
		case c.Error != nil:
			broken = true
		}
	}

	return failed, ran, broken
}

// name is the case's name, after its classname when it has one.
func (c junitCase) name() string {
	if c.Classname == "" {
		return c.Name
	}

	return c.Classname + "::" + c.Name
}

// inTest is whether the error happened in a test's setup or teardown.
func (m junitMessage) inTest() bool {
	return slices.ContainsFunc(testErrors, func(prefix string) bool { return strings.HasPrefix(m.Message, prefix) })
}
