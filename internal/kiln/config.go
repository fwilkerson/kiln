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

// config is kiln's settings for a project, in .kiln/kiln.json. kiln reads them from main, never from a build's
// checkout, so a build cannot weaken its own gate.
type config struct {
	// Each field's doc is the hover an editor shows for its key, as TestSchema builds it. So each stands alone.

	// Schema is the path of the JSON Schema that an editor checks this file against: "./generated/kiln.schema.json".
	// kiln init writes that schema to .kiln/generated/. kiln ignores this key.
	Schema string `json:"$schema,omitempty"`
	// Gate lists the commands that must pass before work lands on main, such as ["go vet ./...", "go test ./..."].
	// kiln runs each with sh -c, from the top of a checkout of the work. kiln refuses a kiln.json that lists no
	// command.
	Gate []string `json:"gate"`
	// Rounds is how many times kiln sends a build back to its session before the build stops, such as 3. kiln sends
	// a build back when its gate fails, its work conflicts with main, or its work changes nothing. The audit's
	// findings go back too, and use no round. Absent or 0, the first failing gate stops the build.
	Rounds int `json:"rounds"`
	// AllowedHosts lists the only hosts that a session's shell commands may connect to, such as
	// ["proxy.golang.org", "*.example.org"]. Write each as a bare host name, with no scheme, port or path. Absent,
	// a session's shell commands connect to no host.
	AllowedHosts []string `json:"allowedHosts,omitempty"`
	// WritablePathCommands lists shell commands that print paths, such as ["go env GOCACHE GOMODCACHE"]. A
	// session's shell commands may write to each printed path, as well as to the session's checkout. kiln runs each
	// listed command with sh -c. kiln reads each word that the command prints as one absolute path. No session
	// starts while a listed command fails, or prints a path that is not absolute. Absent, a session's shell commands
	// write only to the session's checkout.
	WritablePathCommands []string `json:"writablePathCommands,omitempty"`
	// MCPServers lists the only MCP servers a session may call, by name, such as {"docs": {"command": "docs-mcp"}}.
	// Write each server as Claude Code's own mcpServers setting holds one. Each server runs outside the sandbox that
	// confines a session's shell commands. Absent, a session calls no MCP server.
	MCPServers map[string]json.RawMessage `json:"mcpServers,omitempty"`
	// VerifySession, when true, gives each build two sessions. The build session writes the code. The verify session
	// writes the acceptance tests from the spec alone. Neither session reads the other's checkout. Absent or false,
	// one session writes the code and the tests. A build uses the value that main held when the build was queued.
	VerifySession bool `json:"verifySession,omitempty"`
	// AcceptanceTests is the path that holds a spec's acceptance tests, such as "uat/{spec}/". kiln puts the spec's
	// name in place of {spec}. A list of such paths gives each spec several paths. Each path holds some text before
	// {spec}. No listed path's text before {spec} starts with another's. kiln queue refuses every spec while
	// acceptanceTests is absent. A build uses the value that main held when the build was queued.
	AcceptanceTests paths `json:"acceptanceTests,omitempty"`
	// TestHelpers lists paths of helper code that the acceptance tests call, such as ["internal/testutil/"]. With
	// verifySession true, the verify session may write these paths, besides the acceptance tests and the spec's
	// amendment file. The build session may write these paths too. kiln queue refuses a spec whose acceptance path
	// equals, holds or lies under one of these paths. A build uses the value that main held when the build was
	// queued.
	TestHelpers []string `json:"testHelpers,omitempty"`
	// Reaudit lists paths whose changes the audit must read before the work lands, such as ["migrations/"]. The
	// audit reads the work once. When the work changes one of these paths after that, the audit reads the work again.
	// That second reading uses a round. Absent, the audit never reads what a session changes in answer to the audit's
	// findings.
	Reaudit []string `json:"reaudit,omitempty"`
	// Notify is a command that receives each notification, such as ".kiln/notify". A notification says that a build
	// or a fix needs the developer. The command runs with sh -c, and reads the notification as JSON from the
	// environment variable KILN_NOTICE. kiln kills a command that runs past 30 seconds. Absent, or when the command
	// fails, kiln prints the notification as an OSC 9 escape sequence. A terminal shows that sequence as a desktop
	// notification.
	Notify string `json:"notify,omitempty"`
	// Timeout is how long one command that kiln runs for the project may take, such as one gate command. It is also
	// how long a session may print nothing. Past it, kiln kills that command or session. It does not limit a whole
	// build. Write a Go duration above zero, such as "2h". Absent, it is 1 hour.
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

// specVar stands for the spec's name in kiln.json's acceptanceTests.
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
	for _, p := range c.AcceptanceTests {
		filled = append(filled, strings.ReplaceAll(p, specVar, string(spec)))
	}

	return filled
}

// fixedParts are the parts of each acceptance path before {spec}, which are the same for every spec.
func (c config) fixedParts() []string {
	var fixed []string
	for _, p := range c.AcceptanceTests {
		part, _, _ := strings.Cut(p, specVar)
		fixed = append(fixed, part)
	}

	return fixed
}

// acceptanceRefusals say why kiln queue may not take acceptance. Each path holds {spec}, after a part of its own, in
// its clean form. No path's part before {spec} starts another's, so no two specs' paths meet.
func (c config) acceptanceRefusals() refusals {
	if len(c.AcceptanceTests) == 0 {
		return refusals{{configFile + " names no acceptanceTests, the paths of a spec's acceptance tests",
			fmt.Sprintf("add it, such as \"acceptanceTests\": \"acceptance/%s/\"", specVar)}}
	}

	var refused refusals
	for _, p := range c.AcceptanceTests {
		if why, ok := pathRefusal(p); ok {
			refused = append(refused, why.about(fmt.Sprintf("%s names the acceptance path %q,", configFile, p))...)
		}
	}

	fixed := c.fixedParts()
	for i, a := range fixed {
		for j, b := range fixed[i+1:] {
			if strings.HasPrefix(fold(a), fold(b)) || strings.HasPrefix(fold(b), fold(a)) {
				refused = append(refused, refusal{fmt.Sprintf("%s names the acceptance paths %q and %q, whose "+
					"parts before %s could make two specs' paths meet", configFile, c.AcceptanceTests[i],
					c.AcceptanceTests[i+1+j], specVar), "start each with a part no other starts with"})
			}
		}
	}

	return refused
}

// helperRefusals say why kiln queue may not take spec. It refuses a spec whose acceptance paths meet a testHelpers
// path.
// A split build removes what main holds under those paths from its build session's checkout.
func (c config) helperRefusals(spec Spec) refusals {
	var refused refusals
	for _, a := range c.acceptancePaths(spec) {
		for _, h := range c.TestHelpers {
			if meets(a, h) {
				refused = append(refused, refusal{fmt.Sprintf("%s's acceptance path %s meets the testHelpers path %s, "+
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

// unguarded is whether kiln cannot keep a fix off the acceptance tests. Then acceptanceTests names no path, or one with
// no part before {spec}.
func (c config) unguarded() bool {
	return len(c.AcceptanceTests) == 0 || slices.Contains(c.fixedParts(), "")
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

// configRefusals word err, json/v2's error for raw, as kiln's own refusal. A key kiln does not read is named with the
// nearest key it does. A syntax error is named by its line and column. A value of the wrong type is
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

// unknownKey refuses the key sem names, which kiln does not read. It names the key kiln reads that is nearest in
// spelling. kiln.json nests no object whose keys kiln reads.
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
	for _, h := range c.AllowedHosts {
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

// config reads kiln.json at the commit at, which work was queued or started at.
func (r *Repo) config(ctx context.Context, at git.Rev) (config, error) {
	raw, found, err := r.filesAt(at).read(ctx, configFile)
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
