package kiln

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/fwilkerson/practices/claim"
)

func TestReadJUnit(t *testing.T) {
	for file, want := range map[string]struct {
		failed, ran []string
		broken      bool
	}{
		"failure.xml": {
			failed: []string{"tests.test_failure::test_a"},
			ran:    []string{"tests.test_failure::test_a", "tests.test_failure::test_ok"},
		},
		"setup-error.xml": {
			failed: []string{"tests.test_setup::test_a"},
			ran:    []string{"tests.test_setup::test_a", "tests.test_setup::test_ok"},
		},
		"teardown-error.xml": {
			failed: []string{"tests.test_teardown::test_a"},
			ran:    []string{"tests.test_teardown::test_a", "tests.test_teardown::test_ok"},
		},
		"parameter.xml": {
			failed: []string{"tests.test_param::test_a[2]"},
			ran:    []string{"tests.test_param::test_a[1]", "tests.test_param::test_a[2]", "tests.test_param::test_ok"},
		},
		"collection-error.xml": {ran: []string{"tests.test_bad"}, broken: true},
		"skipped.xml": {
			ran: []string{"tests.test_skipped::test_a", "tests.test_skipped::test_ok"},
		},
		"nested.xml": {
			failed: []string{"pkg.test_deep::test_a"},
			ran:    []string{"pkg.test_deep::test_a", "pkg.test_deep::test_ok", "test_top"},
		},
		"node-test.xml": {
			failed: []string{"test::fails"},
			ran:    []string{"test::fails", "test::inner ok", "test::passes"},
		},
		"truncated.xml": {broken: true},
		"missing.xml":   {},
	} {
		t.Run(file, func(t *testing.T) {
			failed, ran, broken := readJUnit(filepath.Join("testdata", "junit", file))
			if !claim.Holds(slices.Equal(failed, want.failed), slices.Equal(ran, want.ran), broken == want.broken) {
				t.Errorf("got failed %q, ran %q, broken %v; want %q, %q, %v",
					failed, ran, broken, want.failed, want.ran, want.broken)
			}
		})
	}
}
