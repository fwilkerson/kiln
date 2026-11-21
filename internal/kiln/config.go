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

// config is kiln's settings for a project, in .kiln/kiln.json. kiln reads them from main's tip, so a build cannot
// weaken the gate it is held to.
type config struct {
	// Each field's doc is the hover an editor shows for its key, as TestSchema builds it. So each stands alone.

	// Schema names the JSON Schema an editor checks this file against: "./kiln.schema.json", which kiln init writes
	// beside it. kiln reads nothing from it.
	Schema string `json:"$schema,omitempty"`
	// Gate holds the commands that must pass before work lands on main, such as ["go vet ./...", "go test ./..."].
	// Each runs with sh -c, at the top of a checkout of the work. kiln refuses a kiln.json without one.
	Gate []string `json:"gate"`
	// Rounds caps how often a failing gate goes back to the session before the build stops, such as 3. The audit's
	// findings spend no round. Absent, or 0, the first failing gate stops the build.
	Rounds int `json:"rounds"`
	// Hosts are the only hosts a session's shell commands may reach, such as ["proxy.golang.org", "*.example.org"].
	// Each is a bare host name, with no scheme, port or path. Absent, those commands reach no host.
	Hosts []string `json:"hosts,omitempty"`
	// Writable holds shell commands, not paths, such as ["go env GOCACHE GOMODCACHE"]. Each runs with sh -c. Each
	// word it prints is an absolute path a session's commands may write. Absent, they write only in the checkout.
	Writable []string `json:"writable,omitempty"`
	// MCP names the only MCP servers a session may call, such as {"docs": {"command": "docs-mcp"}}. Each is written as
	// Claude Code's mcpServers holds one. They run outside the sandbox. Absent, a session calls none.
	MCP map[string]json.RawMessage `json:"mcp,omitempty"`
	// TestSession, when true, has a second session write the acceptance tests from the spec alone. The first writes
	// the code, and neither reads the other's checkout. Without it, one session writes the code and the tests. kiln
	// reads it at the commit the build was queued at.
	TestSession bool `json:"testSession,omitempty"`
	// Acceptance holds each path a spec's acceptance tests live in, with {spec} where the spec's name goes, such as
	// "uat/{spec}/". One path may be a plain string. kiln queue refuses a spec while it is absent. kiln reads it at
	// the commit the build was queued at.
	Acceptance paths `json:"acceptance,omitempty"`
	// Shared holds the paths both sessions may write when testSession is true, such as ["internal/testutil/"]. Name
	// code the tests run through there. Absent, the test session writes only the acceptance tests and the spec's
	// amendments. kiln queue refuses a spec whose acceptance path meets one of these paths. Without testSession, kiln
	// ignores this key. kiln reads it at the commit the build was queued at.
	Shared []string `json:"shared,omitempty"`
	// Reaudit holds the paths the audit reads before each change to them lands, such as ["migrations/"]. When a
	// session's answer to the audit's findings changes one, the audit reads the work again. Absent, the audit reads
	// the work once, and never reads such an answer.
	Reaudit []string `json:"reaudit,omitempty"`
	// Notify is a command that receives each notification, such as ".kiln/notify", run with sh -c. The notice is
	// JSON in $KILN_NOTICE. Absent, kiln prints each notification in its output as OSC 9, which a terminal shows.
	Notify string `json:"notify,omitempty"`
	// Timeout bounds each command kiln runs for the project, such as one gate command, and each session's silence.
	// It never bounds a whole build. Past it, kiln kills that command or session. It is a Go duration above zero,
	// such as "2h". Absent, it is 1 hour.
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

// sharedRefusals say why kiln queue may not take spec. It refuses a spec whose acceptance paths meet a shared path.
// A split build removes what main holds under those paths from its build session's checkout.
func (c config) sharedRefusals(spec Spec) refusals {
	var refused refusals
	for _, a := range c.acceptancePaths(spec) {
		for _, h := range c.Shared {
			if meets(a, h) {
				refused = append(refused, refusal{fmt.Sprintf("%s's acceptance path %s meets the shared path %s, "+
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
	// The strict read below stops at the first unknown key, so each moved key is refused first, all at once.
	var keys map[string]jsontext.Value
	_ = jsonv2.Unmarshal(raw, &keys)
	if moved := oldKeys(configFile, keys, func(key string) string { return movedKeys[key] }); len(moved) > 0 {
		return c, unreadable{moved}
	}

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
	"workflow": "say \"testSession\": true in its place for a second session that writes the acceptance tests, or " +
		"drop it for one session that writes the code and the tests",
	"harness": "name it \"shared\", the paths both sessions may write when testSession is true",
	"gauges": "each gauge is a monitor now, " + monitorsDir + "/<name>/ with its gauge script, and its line in " +
		"monitor.json: move each there, as kiln help gauges shows",
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
// names the key kiln reads that is nearest in spelling. kiln.json nests no object whose keys kiln reads.
func unknownKey(sem *jsonv2.SemanticError) refusal {
	key := sem.JSONPointer.LastToken()
	problem := fmt.Sprintf("%s holds %q, a key kiln does not read", configFile, key)
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
	reflect.String: `a string, such as "x"`, reflect.Bool: "true or false", reflect.Struct: "an object",
	reflect.Map: "an object"}

// typeWords name the JSON value kiln reads into a Go value of type t, with an example. Each list kiln.json holds is of
// strings.
func typeWords(t reflect.Type) string {
	if t.Kind() == reflect.Slice {
		return `a list of strings, such as ["go test ./..."]`
	}

	return cmpOr(scalarWords[t.Kind()], "the type the reference gives it")
}

// refusals say why kiln refuses c, but for its acceptance, which kiln queue alone refuses.
func (c config) refusals() refusals {
	refused := broken(
		rule{len(c.Gate) == 0, refusal{configFile + " names no gate command", "name one, such as \"go test ./...\""}},
		rule{c.Rounds < 0, refusal{fmt.Sprintf("%s names %d rounds", configFile, c.Rounds), "name 0 or more"}},
	)
	for _, h := range c.Hosts {
		if !bareHost(h) {
			refused = append(refused, refusal{fmt.Sprintf("%s names the host %q", configFile, h),
				"name a bare host, such as \"proxy.golang.org\", or \"*.example.org\""})
		}
	}

	return refused
}

// bareHost is whether h names a host as Claude Code's allowedDomains reads one. Such a host holds letters, digits,
// hyphens and dots, after an optional "*." for subdomains. A scheme, a port or a path fails.
func bareHost(h string) bool {
	name := strings.TrimPrefix(h, "*.")
	valid := func(r rune) bool { return r != '-' && r != '.' && !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	return name != "" && !strings.ContainsFunc(name, valid)
}

// config reads kiln.json at the commit at, which work was queued or started at, as pastFiles reads it.
func (r *Repo) config(ctx context.Context, at git.Rev) (config, error) {
	raw, found, err := r.pastFiles(at).read(ctx, configFile)
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

// pastFiles reads the files the commit at holds, where work was queued or started. An older kiln may have queued that
// work, and main's settings no longer bind it. So a key that kiln read, and this kiln renamed, reads as its new key.
func (r *Repo) pastFiles(at git.Rev) files {
	f := r.filesAt(at)
	read := f.read
	f.read = func(ctx context.Context, p string) ([]byte, bool, error) {
		raw, found, err := read(ctx, p)
		switch {
		case err != nil || !found:
			return raw, found, err
		case p == configFile:
			return rekeyed(raw, pastConfig), true, nil
		case path.Base(p) == settingsFile:
			return rekeyed(raw, pastSettings), true, nil
		}

		return raw, true, nil
	}

	return f
}

// pastConfig moves each key of kiln.json that an older kiln read to the key in its place. A setting that moved out of
// kiln.json reads as absent, since a past commit's monitors are read from their directories.
func pastConfig(keys map[string]jsontext.Value) {
	var workflow string
	if jsonv2.Unmarshal(keys["workflow"], &workflow) == nil && workflow == "split" {
		keys["testSession"] = jsontext.Value("true")
	}

	if shared, ok := keys["harness"]; ok {
		keys["shared"] = shared
	}

	for key := range movedKeys {
		delete(keys, key)
	}
}

// pastSettings moves each key of a monitor.json that an older kiln read to the key in its place.
func pastSettings(keys map[string]jsontext.Value) {
	for old, now := range renamedSettings {
		if v, ok := keys[old]; ok {
			keys[now] = v
			delete(keys, old)
		}
	}
}

// rekeyed is raw, a JSON object, with its keys as edit leaves them. It answers raw that is no object unchanged, for
// the strict read to refuse.
func rekeyed(raw []byte, edit func(keys map[string]jsontext.Value)) []byte {
	var keys map[string]jsontext.Value
	if jsonv2.Unmarshal(raw, &keys) != nil {
		return raw
	}

	edit(keys)
	if out, err := jsonv2.Marshal(keys); err == nil {
		return out
	}

	return raw
}

// readConfig parses raw, kiln.json as a commit holds it. It refuses a commit that holds none, where found is false.
func readConfig(raw []byte, found bool) (config, error) {
	if !found {
		return config{}, fmt.Errorf("main holds no %s: %s", configFile, initHint)
	}

	return parseConfig(raw)
}
