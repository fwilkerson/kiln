package kiln

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
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
	// Schema names the JSON Schema an editor checks kiln.json against, ./kiln.schema.json, which kiln init writes.
	// kiln reads nothing from it.
	Schema string `json:"$schema,omitempty"`
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

// acceptanceRefusals say why kiln queue may not take acceptance. Each path holds {spec}, after a part of its own, in
// its clean form. No path's part before {spec} starts another's, so no two specs' paths meet.
func (c config) acceptanceRefusals() refusals {
	if len(c.Acceptance) == 0 {
		return refusals{{configFile + " names no acceptance, the paths of a spec's acceptance tests",
			fmt.Sprintf("add one, such as \"acceptance/%s/\"", specVar)}}
	}

	var refused refusals
	for _, p := range c.Acceptance {
		if why, ok := pathRefusal(p); ok {
			refused = append(refused, why.about(fmt.Sprintf("%s names the acceptance path %q,", configFile, p))...)
		}
	}

	fixed := c.fixedParts()
	for i, a := range fixed {
		for j, b := range fixed[i+1:] {
			if strings.HasPrefix(fold(a), fold(b)) || strings.HasPrefix(fold(b), fold(a)) {
				refused = append(refused, refusal{fmt.Sprintf("%s names the acceptance paths %q and %q, whose "+
					"parts before %s could make two specs' paths meet", configFile, c.Acceptance[i],
					c.Acceptance[i+1+j], specVar), "start each with a part no other starts with"})
			}
		}
	}

	return refused
}

// harnessRefusals say why kiln queue may not take spec. It refuses a spec whose acceptance paths meet a harness path.
// A split build removes what main holds under those paths from its build session's checkout.
func (c config) harnessRefusals(spec Spec) refusals {
	var refused refusals
	for _, a := range c.acceptancePaths(spec) {
		for _, h := range c.Harness {
			if meets(a, h) {
				refused = append(refused, refusal{fmt.Sprintf("%s's acceptance path %s meets the harness path %s, "+
					"which its build would remove", spec, a, h), "rename the spec"})
			}
		}
	}

	return refused
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

// pathRefusal says why kiln.json may not name p as an acceptance path. It answers false for a path it may name.
func pathRefusal(p string) (refusals, bool) {
	example := fmt.Sprintf("such as \"uat/%s/\"", specVar)
	switch {
	case !strings.Contains(p, specVar):
		return refusals{{"which lacks " + specVar + ", so it would hold every spec's tests",
			"put " + specVar + " where each spec's name goes, " + example}}, true
	case strings.HasPrefix(p, specVar):
		return refusals{{"which starts with " + specVar + ", so a spec's name would decide where it is",
			"put a directory of its own before " + specVar + ", " + example}}, true
	// fs.ValidPath refuses a leading slash, an unclean form, and ".." that would climb out of the repository.
	case !fs.ValidPath(strings.TrimSuffix(p, "/")):
		return refusals{{"which is not a clean path from the repository's top",
			"write it as path.Clean does, " + example}}, true
	}

	return nil, false
}

const configFile = kilnDir + "/kiln.json"

// parseConfig reads kiln.json. A file that is no JSON object of its keys, each as kiln.schema.json spells it, fails
// as unreadable. Otherwise the error holds each of its refusals, and c is what the file holds.
func parseConfig(raw []byte) (c config, err error) {
	// json/v2 matches a key's case exactly, as the schema does, where v1 would take "Gate" for gate.
	if err := jsonv2.Unmarshal(raw, &c, jsonv2.RejectUnknownMembers(true)); err != nil {
		return c, unreadable{refusals{{fmt.Sprintf("%s: %v", configFile, err),
			"fix its JSON, and spell each key as kiln.schema.json does"}}}
	}

	return c, c.refusals().err()
}

// refusals say why kiln refuses c, but for its acceptance, which kiln queue alone refuses.
func (c config) refusals() refusals {
	refused := broken(
		rule{len(c.Gate) == 0, refusal{configFile + " names no gate command", "name one, such as \"go test ./...\""}},
		rule{c.Rounds < 0, refusal{fmt.Sprintf("%s names %d rounds", configFile, c.Rounds), "name 0 or more"}},
		rule{c.Workflow != "" && c.Workflow != splitWorkflow, refusal{fmt.Sprintf("%s names the workflow %q",
			configFile, c.Workflow), fmt.Sprintf("name %q, or none for build then audit", splitWorkflow)}},
	)
	for _, h := range c.Hosts {
		if !bareHost(h) {
			refused = append(refused, refusal{fmt.Sprintf("%s names the host %q", configFile, h),
				"name a bare host, such as \"proxy.golang.org\", or \"*.example.org\""})
		}
	}

	refused = append(refused, c.gaugeRefusals()...)
	return append(refused, c.monitorsRefusals()...)
}

// bareHost is whether h names a host as Claude Code's allowedDomains reads one. Such a host holds letters, digits,
// hyphens and dots, after an optional "*." for subdomains. A scheme, a port or a path fails.
func bareHost(h string) bool {
	name := strings.TrimPrefix(h, "*.")
	valid := func(r rune) bool { return r != '-' && r != '.' && !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	return name != "" && !strings.ContainsFunc(name, valid)
}

// gaugeRefusals refuse a gauge with no name, a name another gauge has, or not exactly one of min and max.
func (c config) gaugeRefusals() refusals {
	var refused refusals
	named := map[string]bool{}
	for _, g := range c.Gauges {
		refused = append(refused, broken(
			rule{g.Name == "", refusal{"has no name", "name it"}},
			rule{g.Name != "" && named[g.Name], refusal{"has the name of a gauge before it", "rename one"}},
			rule{(g.Min == nil) == (g.Max == nil), refusal{"names both min and max, or neither",
				"name one line, min or max"}},
		).about(fmt.Sprintf("%s names a gauge %q that", configFile, g.Name))...)
		named[g.Name] = true
	}

	return refused
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
