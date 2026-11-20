package scenariotest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// seededConfig holds the keys a script's .kiln/kiln.json gets when it lacks them: a Go project's, built in 1 round.
var seededConfig = map[string]json.RawMessage{
	"acceptance": json.RawMessage(`"acceptance/{spec}/"`),
	"gate":       json.RawMessage(`["go vet ./...", "go test ./..."]`),
	"rounds":     json.RawMessage(`1`),
}

// seededModule is the go.mod a script gets when it lacks one and takes the seeded gate, which is Go's.
const seededModule = "module example.com/scratch\n\ngo 1.27\n"

// seededAgents are the agents' files a script gets, by path, when it lacks that file.
var seededAgents = map[string]string{
	".kiln/agents/build.md":  "Build the spec.\n",
	".kiln/agents/verify.md": "Write the acceptance tests.\n",
	".kiln/agents/audit.md":  "---\ntools: Read, Grep, Glob, Bash\n---\n\nAudit the packages.\n",
}

// seededSpec is the spec a short one becomes. Its verbs are the spec's number, its name and its acceptance tests.
const seededSpec = "# Spec %d: %s\n\nStatus: Approved, 2026-11-09\n\n## Overview\n\n## Goals\n\n## Non-goals\n\n" +
	"## Open Questions\n\n## Acceptance tests\n\n%s\n## Technical Plan\n\n## Alternatives Considered\n"

// shortSpec opens a spec that holds only its acceptance tests.
const shortSpec = "### "

// seed gives the scratch repository at work what its script left out, if the script brings anything under .kiln/.
func seed(work string) error {
	if _, err := os.Stat(filepath.Join(work, ".kiln")); err != nil {
		return nil
	}

	for _, step := range []func(string) error{seedConfig, seedAgents, seedSpecs, seedMonitors} {
		if err := step(work); err != nil {
			return fmt.Errorf("scenariotest: seeding the scratch repository: %w", err)
		}
	}

	return nil
}

// seedConfig gives .kiln/kiln.json each key of seededConfig it lacks, and the repository go.mod with the seeded gate.
// It leaves a kiln.json that is no JSON object as the script wrote it.
func seedConfig(work string) error {
	path := filepath.Join(work, ".kiln", "kiln.json")
	config := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(path); err == nil && json.Unmarshal(raw, &config) != nil {
		return nil
	}

	if _, own := config["gate"]; !own {
		if err := writeMissing(filepath.Join(work, "go.mod"), seededModule); err != nil {
			return err
		}
	}

	seeded := maps.Clone(seededConfig)
	maps.Copy(seeded, config)
	raw, err := json.MarshalIndent(seeded, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// seedAgents writes each of seededAgents that the repository lacks.
func seedAgents(work string) error {
	for p, content := range seededAgents {
		if err := writeMissing(filepath.Join(work, p), content); err != nil {
			return err
		}
	}

	return nil
}

// seedSpecs makes each short spec under .kiln/specs/ a whole one, numbered by its place among the specs there.
func seedSpecs(work string) error {
	specs, err := filepath.Glob(filepath.Join(work, ".kiln", "specs", "*.md"))
	if err != nil {
		return err
	}

	specs = slices.DeleteFunc(specs, func(p string) bool { return strings.HasSuffix(p, ".amend.md") })
	slices.Sort(specs)
	for i, path := range specs {
		tests, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if !bytes.HasPrefix(tests, []byte(shortSpec)) {
			continue
		}

		name := strings.TrimSuffix(filepath.Base(path), ".md")
		if err := os.WriteFile(path, fmt.Appendf(nil, seededSpec, i+1, name, tests), 0o644); err != nil {
			return err
		}
	}

	return nil
}

// writeMissing writes content to path, with its directory, unless a file is there.
func writeMissing(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(content), 0o644)
}

// seedMonitors makes each monitor's check, gauge and command executable, since a script's files keep no mode. kiln
// runs each by its path.
func seedMonitors(work string) error {
	for _, script := range []string{"check", "gauge", "command"} {
		found, err := filepath.Glob(filepath.Join(work, ".kiln", "monitors", "*", script))
		if err != nil {
			return err
		}

		for _, p := range found {
			if err := os.Chmod(p, 0o755); err != nil {
				return err
			}
		}
	}

	return nil
}
