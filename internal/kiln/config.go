package kiln

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
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
	// Notify is the command that receives each notification, run with sh -c. "" prints each as OSC 9.
	Notify string `json:"notify,omitempty"`
	// Timeout is how long a project's command may run, or a session print nothing, before kiln kills it. It is a Go
	// duration above zero, such as "2h", or unset for kiln's default.
	Timeout timeout `json:"timeout,omitempty"`
}

// defaultTimeout is the timeout of a kiln.json that names none. It is over twice the longest gate seen on a loaded
// machine.
const defaultTimeout = time.Hour

// timeoutKey names the setting in the messages of what kiln killed.
const timeoutKey = "kiln.json's timeout"

// timeout is a Go duration above zero, such as "2h". kiln.json refuses any other.
type timeout time.Duration

func (t *timeout) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if d, err := time.ParseDuration(text); err == nil && d > 0 {
			*t = timeout(d)
			return nil
		}
	}

	return refusals{{fmt.Sprintf("%s names the timeout %s, which is no Go duration above zero", configFile, raw),
		fmt.Sprintf("name one such as \"2h\", or none for %s", shownDuration(defaultTimeout))}}
}

// killAfter is how long a project's command may run, or a session print nothing, before kiln kills it.
func (c config) killAfter() time.Duration {
	return cmpOr(time.Duration(c.Timeout), defaultTimeout)
}

// shell runs the project's commands with env, a kiln's environment, for as long as c lets one run.
func (c config) shell(env []string) projectShell {
	return projectShell{env: proc.Shell.Env(env), limit: c.killAfter(), bound: timeoutKey}
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

// parseConfig reads kiln.json. A file that is no JSON object of the keys kiln reads fails as unreadable, as
// configRefusals words it. Otherwise the error holds each of its refusals, and c is what the file holds.
func parseConfig(raw []byte) (c config, err error) {
	// json/v2 matches a key's case exactly, as the schema does, where v1 would take "Gate" for gate.
	if err := jsonv2.Unmarshal(raw, &c, jsonv2.RejectUnknownMembers(true)); err != nil {
		return c, unreadable{configRefusals(raw, err)}
	}

	return c, c.refusals().err()
}

// movedKeys are the keys of kiln.json that kiln once read, each with where its setting went.
var movedKeys = map[string]string{
	"monitors": "each monitor is a directory of its own now, " + monitorsDir + "/<name>/: move each there, or add one " +
		"with kiln monitor add",
}

// configRefusals word err, json/v2's error for raw, as kiln's own refusal. A key kiln does not read is named with the
// nearest key it does, or where it went. A syntax error is named by its line and column. A value of the wrong type is
// named with the type its key takes.
func configRefusals(raw []byte, err error) refusals {
	if own, ok := errors.AsType[refusals](err); ok {
		return own
	}

	if syn, ok := errors.AsType[*jsontext.SyntacticError](err); ok {
		return refusals{syntaxRefusal(raw, syn)}
	}

	sem, ok := errors.AsType[*jsonv2.SemanticError](err)
	switch {
	case !ok:
		return refusals{{fmt.Sprintf("%s: %v", configFile, err), "fix its JSON"}}
	case errors.Is(err, jsonv2.ErrUnknownName):
		return refusals{unknownKey(sem)}
	}

	at := strings.TrimPrefix(string(sem.JSONPointer), "/")
	return refusals{{fmt.Sprintf("%s's %s is %s", configFile, at, kindWords[sem.JSONKind]), "make it " +
		typeWords(sem.GoType)}}
}

// syntaxRefusal names the line and the column of syn, a syntax error in raw.
func syntaxRefusal(raw []byte, syn *jsontext.SyntacticError) refusal {
	at := min(int(syn.ByteOffset), len(raw))
	line := bytes.Count(raw[:at], []byte("\n")) + 1
	column := at - bytes.LastIndexByte(raw[:at], '\n')
	what := syn.Err.Error()
	// jsontext names a comma before a closing bracket as a value that starts badly.
	if trailingComma(raw[at:]) {
		what = "a comma with nothing after it"
	}

	return refusal{fmt.Sprintf("%s is no JSON at line %d, column %d: %s", configFile, line, column, what),
		"fix its JSON there"}
}

// trailingComma is whether text, from a syntax error on, starts with a comma that only a closing bracket follows.
func trailingComma(text []byte) bool {
	rest, comma := bytes.CutPrefix(text, []byte(","))
	closes := bytes.IndexAny(bytes.TrimLeft(rest, " \t\r\n"), "}]") == 0
	return comma && closes
}

// unknownKey refuses the key sem names, which kiln does not read. It says where a key kiln once read went. Otherwise it
// names the key kiln reads there that is nearest in spelling.
func unknownKey(sem *jsonv2.SemanticError) refusal {
	key, parent := sem.JSONPointer.LastToken(), strings.TrimPrefix(string(sem.JSONPointer.Parent()), "/")
	if went, ok := movedKeys[key]; ok && parent == "" {
		return refusal{fmt.Sprintf("%s holds %q, a key kiln no longer reads", configFile, key), went}
	}

	where := ""
	if parent != "" {
		where = " in " + parent
	}

	problem := fmt.Sprintf("%s holds %q%s, a key kiln does not read", configFile, key, where)
	keys := jsonKeys(sem.GoType)
	if near := nearest(key, keys); near != "" {
		return refusal{problem, fmt.Sprintf("name it %q", near)}
	}

	return refusal{problem, "remove it, or name one kiln reads: " + strings.Join(keys, ", ")}
}

// jsonKeys are the keys kiln reads in an object of the struct type t, in the struct's order.
func jsonKeys(t reflect.Type) []string {
	var keys []string
	for f := range t.Fields() {
		if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "" && name != "-" {
			keys = append(keys, name)
		}
	}

	return keys
}

// nearest is the one of keys that key misspells, by case or by two letters at most. It answers "" for none.
func nearest(key string, keys []string) string {
	best, bestCost := "", 3
	for _, k := range keys {
		if cost := editCost(strings.ToLower(key), strings.ToLower(k)); cost < bestCost {
			best, bestCost = k, cost
		}
	}

	return best
}

// editCost is how many letters to insert, delete or replace to turn a into b.
func editCost(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}

	for i := 1; i <= len(a); i++ {
		diagonal := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			replace := diagonal + boolInt(a[i-1] != b[j-1])
			diagonal = row[j]
			row[j] = min(row[j]+1, row[j-1]+1, replace)
		}
	}

	return row[len(b)]
}

// boolInt is 1 for true, and 0 for false.
func boolInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

// kindWords name each kind of JSON value, as a refusal of one of the wrong type says it is.
var kindWords = map[jsontext.Kind]string{jsontext.KindNull: "null", jsontext.KindFalse: "false",
	jsontext.KindTrue: "true", jsontext.KindString: "a string", jsontext.KindNumber: "a number",
	jsontext.KindBeginObject: "an object", jsontext.KindBeginArray: "a list"}

// scalarWords name the JSON value each kind of Go value reads from, with an example.
var scalarWords = map[reflect.Kind]string{reflect.Int: "a whole number, such as 3",
	reflect.Float64: "a number, such as 0.5", reflect.String: `a string, such as "x"`,
	reflect.Bool: "true or false", reflect.Struct: "an object", reflect.Map: "an object"}

// typeWords name the JSON value kiln reads into a Go value of type t, with an example.
func typeWords(t reflect.Type) string {
	switch {
	case t.Kind() == reflect.Pointer:
		return typeWords(t.Elem())
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.String:
		return `a list of strings, such as ["go test ./..."]`
	case t.Kind() == reflect.Slice:
		return "a list of objects"
	}

	return cmpOr(scalarWords[t.Kind()], "the type the reference gives it")
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

	return append(refused, c.gaugeRefusals()...)
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
			rule{g.sides() != 1, refusal{"names both min and max, or neither",
				"name one line, min or max"}},
		).about(fmt.Sprintf("%s names a gauge %q that", configFile, g.Name))...)
		named[g.Name] = true
	}

	return refused
}

// gaugeNamed is the gauge of c named name.
func (c config) gaugeNamed(name string) (gauge, bool) {
	i := slices.IndexFunc(c.Gauges, func(g gauge) bool { return g.Name == name })
	if i < 0 {
		return gauge{}, false
	}

	return c.Gauges[i], true
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
