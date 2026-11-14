// Package uat_test holds kiln's acceptance tests. Each is a script that runs kiln as its developer does.
//
//	go test ./uat -run 'TestUAT/gauges/readings'
//
// testdata/<feature>/<scenario>.txtar is a scenario of a feature. Its comments say its Given, When and Then. It came
// from a spec the developer accepted, and no longer follows that spec's wording or numbers.
//
// A spec not yet accepted keeps its tests in its own package, uat/<spec>/, where TestAT<N> is test N.
// docs/practices.md "Acceptance tests" says when and how they fold in here.
package uat_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestUAT runs each feature's scenarios.
func TestUAT(t *testing.T) { scenariotest.RunAll(t, "testdata") }
