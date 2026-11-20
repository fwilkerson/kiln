package kiln

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"unicode"

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
	// Acceptance holds each path a spec's acceptance tests live in, with {spec} for its name, such as
	// "acceptance/{spec}/". A JSON string is a list of one.
	Acceptance paths `json:"acceptance,omitempty"`
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

// paths is a list of paths in kiln.json, which also reads a JSON string as a list of one. A commit from before
// acceptance took a list holds a string.
type paths []string

func (p *paths) UnmarshalJSON(raw []byte) error {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		*p = paths{one}
		return nil
	}

	return json.Unmarshal(raw, (*[]string)(p))
}

// acceptancePaths are the paths the acceptance tests of spec live in.
func (c config) acceptancePaths(spec Spec) []string {
	var filled []string
	for _, p := range c.Acceptance {
		filled = append(filled, strings.ReplaceAll(p, specVar, string(spec)))
	}

	return filled
}

// fixedParts are the parts of each acceptance path before {spec}, which are the same for every spec.
func (c config) fixedParts() []string {
	var fixed []string
	for _, p := range c.Acceptance {
		part, _, _ := strings.Cut(p, specVar)
		fixed = append(fixed, part)
	}

	return fixed
}

// acceptanceRefusal says why kiln queue may not take acceptance, or "". Each path holds {spec}, after a part of its
// own, in its clean form. No path's part before {spec} starts another's, so no two specs' paths meet.
func (c config) acceptanceRefusal() string {
	if len(c.Acceptance) == 0 {
		return fmt.Sprintf("names no acceptance, the paths of a spec's acceptance tests: add one, such as "+
			"\"acceptance/%s/\"", specVar)
	}

	for _, p := range c.Acceptance {
		if why := pathRefusal(p); why != "" {
			return fmt.Sprintf("names the acceptance path %q, %s", p, why)
		}
	}

	fixed := c.fixedParts()
	for i, a := range fixed {
		for j, b := range fixed {
			if i != j && strings.HasPrefix(fold(a), fold(b)) {
				return fmt.Sprintf("names the acceptance paths %q and %q, whose parts before %s could make two "+
					"specs' paths meet: start each with a part no other starts with", c.Acceptance[j],
					c.Acceptance[i], specVar)
			}
		}
	}

	return ""
}

// harnessRefusal says why kiln queue may not take spec, or "". It refuses a spec whose acceptance paths meet a
// harness path. A split build removes what main holds under those paths from its build session's checkout.
func (c config) harnessRefusal(spec Spec) string {
	for _, a := range c.acceptancePaths(spec) {
		for _, h := range c.Harness {
			if meets(a, h) {
				return fmt.Sprintf("%s's acceptance path %s meets the harness path %s, which its build would "+
					"remove: rename the spec", spec, a, h)
			}
		}
	}

	return ""
}

// meets is whether paths a and b are one path, or one holds the other, as APFS folds case.
func meets(a, b string) bool {
	fa, fb := fold(path.Clean(a))+"/", fold(path.Clean(b))+"/"
	return strings.HasPrefix(fa, fb) || strings.HasPrefix(fb, fa)
}

// unguarded is whether kiln cannot keep a fix off the acceptance tests. Then acceptance names no path, or one with
// no part before {spec}.
func (c config) unguarded() bool {
	return len(c.Acceptance) == 0 || slices.Contains(c.fixedParts(), "")
}

// pathRefusal says why kiln.json may not name p as an acceptance path, or "".
func pathRefusal(p string) string {
	switch {
	case !strings.Contains(p, specVar):
		return "which lacks " + specVar + ", so it would hold every spec's tests"
	case strings.HasPrefix(p, specVar):
		return "which starts with " + specVar + ", so a spec's name would decide where it is"
	// fs.ValidPath refuses a leading slash, an unclean form, and ".." that would climb out of the repository.
	case !fs.ValidPath(strings.TrimSuffix(p, "/")):
		return "which is not a clean path from the repository's top: write it as path.Clean does, such as " +
			"\"uat/" + specVar + "/\""
	}

	return ""
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

	for _, h := range c.Hosts {
		if !bareHost(h) {
			return c, fmt.Errorf("%s names the host %q: name a bare host, such as \"proxy.golang.org\", or "+
				"\"*.example.org\"", configFile, h)
		}
	}

	if err := c.checkGauges(); err != nil {
		return c, err
	}

	return c, c.checkMonitors()
}

// bareHost is whether h names a host as Claude Code's allowedDomains reads one. Such a host holds letters, digits,
// hyphens and dots, after an optional "*." for subdomains. A scheme, a port or a path fails.
func bareHost(h string) bool {
	name := strings.TrimPrefix(h, "*.")
	valid := func(r rune) bool { return r != '-' && r != '.' && !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	return name != "" && !strings.ContainsFunc(name, valid)
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

	return readConfig(raw, found)
}

// mainConfig answers main's tip, as mainTip does, and kiln.json there, as config reads it. One git reads both.
func (r *Repo) mainConfig(ctx context.Context) (git.Rev, config, error) {
	main, files, err := r.git.ShowTip(ctx, mainBranch, configFile)
	switch {
	case err != nil:
		return "", config{}, err
	case main == "":
		return "", config{}, errNoMain
	}

	raw, found := files[configFile]
	c, err := readConfig(raw, found)
	return main, c, err
}

// readConfig parses raw, kiln.json as a commit holds it. It refuses a commit that holds none, where found is false.
func readConfig(raw []byte, found bool) (config, error) {
	if !found {
		return config{}, fmt.Errorf("main holds no %s: %s", configFile, initHint)
	}

	return parseConfig(raw)
}
