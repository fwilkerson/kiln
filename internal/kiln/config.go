package kiln

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fwilkerson/kiln/internal/git"
)

// config is .kiln/kiln.json, read from main's tip, so a build cannot weaken the gate it is held to.
type config struct {
	// Gate holds the commands the gate runs, each with sh -c, at the top of a worktree.
	Gate []string `json:"gate"`
	// Rounds is how many times a failing gate goes back to the session before the build stops.
	Rounds int `json:"rounds"`
	// Hosts are the only hosts a session's commands reach.
	Hosts []string `json:"hosts,omitempty"`
	// Writable holds commands, each run with sh -c. Each field one prints is a path a session's commands may write.
	Writable []string `json:"writable,omitempty"`
	// MCP holds the only MCP servers a session reaches, by name, each as Claude Code's mcpServers holds one. They run
	// outside the sandbox.
	MCP map[string]json.RawMessage `json:"mcp,omitempty"`
	// Gauges are read after each landing, and never block one.
	Gauges []gauge `json:"gauges,omitempty"`
	// Workflow is "split", or "" for build then audit. Like Acceptance and Harness, kiln reads it at the commit the
	// build was queued at.
	Workflow string `json:"workflow,omitempty"`
	// Acceptance is where a spec's acceptance tests live, with {spec} for its name, such as "acceptance/{spec}/".
	Acceptance string `json:"acceptance,omitempty"`
	// Harness holds paths both sessions of a split build may write, such as the code the acceptance tests run through.
	Harness []string `json:"harness,omitempty"`
	// Reaudit holds paths the audit reads again whenever the work's own change to them differs from what it read.
	Reaudit []string `json:"reaudit,omitempty"`
	// Monitors are the jobs kiln monitor run runs on their schedule, each of which may start a fix.
	Monitors []monitor `json:"monitors,omitempty"`
}

// splitWorkflow is the workflow whose build session writes the code, and whose verify session writes the tests.
const splitWorkflow = "split"

// specVar stands for the spec's name in kiln.json's acceptance.
const specVar = "{spec}"

// acceptancePath is where the acceptance tests of spec live.
func (c config) acceptancePath(spec Spec) string {
	return strings.ReplaceAll(c.Acceptance, specVar, string(spec))
}

// fixedPart is the part of acceptance before {spec}, which is the same for every spec.
func (c config) fixedPart() string {
	fixed, _, _ := strings.Cut(c.Acceptance, specVar)
	return fixed
}

const configFile = kilnDir + "/kiln.json"

func parseConfig(raw []byte) (config, error) {
	var c config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("%s: %w", configFile, err)
	}

	if len(c.Gate) == 0 || c.Rounds < 0 {
		return c, fmt.Errorf("%s names no gate command, or fewer than 0 rounds", configFile)
	}

	if c.Workflow != "" && c.Workflow != splitWorkflow {
		return c, fmt.Errorf("%s names the workflow %q: name %q, or none for build then audit", configFile, c.Workflow,
			splitWorkflow)
	}

	if err := c.checkGauges(); err != nil {
		return c, err
	}

	return c, c.checkMonitors()
}

// checkGauges refuses a gauge with no name, a name another gauge has, or not exactly one of min and max.
func (c config) checkGauges() error {
	named := map[string]bool{}
	for _, g := range c.Gauges {
		switch {
		case g.Name == "", named[g.Name], (g.Min == nil) == (g.Max == nil):
			return fmt.Errorf("%s names a gauge with no name, a name another gauge has, or not exactly one of "+
				"min and max: %q", configFile, g.Name)
		}

		named[g.Name] = true
	}

	return nil
}

func (r *Repo) config(ctx context.Context, at git.Rev) (config, error) {
	raw, found, err := r.git.Show(ctx, at, configFile)
	if err != nil {
		return config{}, err
	}

	if !found {
		return config{}, fmt.Errorf("main holds no %s: %s", configFile, initHint)
	}

	return parseConfig(raw)
}
