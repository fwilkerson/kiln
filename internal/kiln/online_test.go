package kiln

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/practices/claim"
)

// TestOnline holds what kiln reads as a connection to Claude's API. Any HTTP reply is one, whatever its status. So
// is a reply that comes after asks that got none, within onlineFor.
func TestOnline(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status int
		// unanswered counts the first requests the API closes with no reply.
		unanswered int32
	}{
		"a reply of 404":                  {status: http.StatusNotFound},
		"a reply of 500":                  {status: http.StatusInternalServerError},
		"a reply after two that got none": {status: http.StatusNotFound, unanswered: 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var asked atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if asked.Add(1) <= tc.unanswered {
					conn, _, _ := http.NewResponseController(w).Hijack()
					_ = conn.Close()
					return
				}

				w.WriteHeader(tc.status)
			}))

			t.Cleanup(api.Close)
			r := &Repo{env: []string{baseURLVar + "=" + api.URL + "/v1"}}
			online, host := r.online(t.Context())
			if !claim.Holds(online, host == strings.TrimPrefix(api.URL, "http://"), asked.Load() == tc.unanswered+1) {
				t.Errorf("online answered %v, asking %s %d times; want a connection, asked %d times", online, host,
					asked.Load(), tc.unanswered+1)
			}
		})
	}
}

// offlineURL is the URL of an API that answers no request, as with no connection.
func offlineURL(t *testing.T) string {
	api := httptest.NewServer(http.NotFoundHandler())
	api.Close()
	return api.URL
}

// TestOnlyDueAgentsAsk holds that kiln asks for a connection only before the due run of a monitor with agent.md. A run
// by name, and a monitor with no agent.md, run the check with no connection.
func TestOnlyDueAgentsAsk(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		named Spec
		fixer map[string]string
	}{
		"a run by name":              {named: "m", fixer: map[string]string{monitorAgent: "Fix it.\n"}},
		"a monitor with no agent.md": {fixer: map[string]string{commandScript: "true\n"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo, ran := t.TempDir(), filepath.Join(t.TempDir(), "ran")
			shell(t, repo, "git init --quiet -b main && git config user.name t && git config user.email t@t")
			writeAll(t, repo, map[string]string{configFile: `{"gate": ["true"], "acceptanceTests": "a/{spec}/"}`})
			tc.fixer[checkScript] = "echo ran >>" + ran + "\n" + checkHealthy + "\n"
			writeMonitor(t, repo, "m", tc.fixer)
			shell(t, repo, "git add -A && git commit --quiet -m start")
			r, err := Open(t.Context(), repo, append(os.Environ(), baseURLVar+"="+offlineURL(t)))
			if err != nil {
				t.Fatal(err)
			}

			var out strings.Builder
			start := time.Now()
			err = r.MonitorRun(t.Context(), tc.named, &out)
			_, statErr := os.Stat(ran)
			if !claim.Holds(err == nil, statErr == nil, time.Since(start) < onlineFor) {
				t.Errorf("kiln monitor run answered %v after %s, and said:\n%s\nwant the check run, with no ask for a "+
					"connection", err, time.Since(start), &out)
			}
		})
	}
}

// TestSkipChangesNoStanding holds that a skipped monitor line is no run. After it, the monitor is as due, as held and
// as stuck as before it.
func TestSkipChangesNoStanding(t *testing.T) {
	ran := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	m := monitor{Name: "m", finder: checkScript, command: true, agent: true, Hours: defaultHours}
	line := func(found string) entry {
		return entry{Kind: monitorKind, Monitor: "m", At: ran, Main: "a", Entry: "e", Found: found, Build: "b"}
	}

	stuck := line(foundCouldNot)
	stuck.Past = true
	for name, lines := range map[string][]entry{
		"a fix that stopped": {line(foundFix), {Kind: outcomeKind, Build: "b", Outcome: outStopped}},
		"a stuck check":      {stuck},
		"a healthy run":      {line(foundHealthy)},
		"a fix whose session lost its connection": {line(foundFix),
			{Kind: outcomeKind, Build: "b", Outcome: outOffline}},
	} {
		t.Run(name, func(t *testing.T) {
			skip := entry{Kind: monitorKind, Monitor: "m", At: ran.Add(time.Hour), Main: "b", Found: foundSkipped}
			now, later := entry{Main: "a", Entry: "e"}, ran.Add(2*time.Hour)
			type standing struct {
				due, stuck bool
				held       string
			}
			standOf := func(h *history) standing {
				return standing{h.due(m, later), h.stuckAt(now), h.holds(m, now)}
			}

			before := standOf(histories(lines)["m"])
			after := standOf(histories(append(lines, skip, skip))["m"])
			if after != before {
				t.Errorf("after a skip, the monitor stands %+v; want %+v, as before it", after, before)
			}
		})
	}
}

// TestNotifiesAcrossLostConnections holds that a session that fails the same way twice notifies once. That holds with
// two fixes between, each ended with no connection.
func TestNotifiesAcrossLostConnections(t *testing.T) {
	var lines []entry
	for i, outcome := range []string{outFailed, outOffline, outOffline} {
		build := string(rune('a' + i))
		lines = append(lines, entry{Kind: monitorKind, Monitor: "m", Found: foundFix, Build: build},
			entry{Kind: outcomeKind, Build: build, Outcome: outcome})
	}

	if notifies(outFailed, histories(lines)["m"]) {
		t.Error("a session that failed as the monitor's last connected run did notifies; want nothing notified")
	}

	if !notifies(outFailed, histories(lines[2:])["m"]) {
		t.Error("a session that failed after fixes that only lost their connection notifies nothing; want a notice")
	}
}

// TestLostConnection holds that a fix's session that fails with no connection answers lostConnection, with the
// session's error. The audit's run shows it, as each session of a fix runs through talk. A run with a connection, or
// a spec's build's, is read as any failed run.
func TestLostConnection(t *testing.T) {
	t.Parallel()
	const why = "API Error: Connection error (ENOTFOUND)"
	for name, tc := range map[string]struct {
		fix, online bool
		// want is the lost connection's why, or "" for a run read as any failed run.
		want string
	}{
		"a fix's audit with no connection":  {fix: true, want: why},
		"a fix's audit with a connection":   {fix: true, online: true},
		"a spec's audit with no connection": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, _, main := landingBuild(t)
			replies(t, b, `{"type":"result","is_error":true,"result":"`+why+`"}`)
			api := cmpOr(map[bool]string{true: os.Getenv(baseURLVar)}[tc.online], offlineURL(t))
			b.r.env, b.auditing = append(b.r.env, baseURLVar+"="+api), main
			if tc.fix {
				b.fix = &fixing{}
			}

			l := claude.Launch{Session: claude.NewID(), Prompt: "Audit it.", Dir: string(b.tree), Role: auditRole,
				Silence: time.Minute}
			_, err := b.talk(t.Context(), l, reason{Why: whyAudit, Try: 1}, nil)
			var lost string
			if l, isLost := errors.AsType[*lostConnection](err); isLost {
				lost, err = l.why, nil
			}

			if !claim.Holds(lost == tc.want, err == nil) {
				t.Errorf("the audit's talk answered %v, as a lost connection %q; want one %q, or no error", err, lost,
					tc.want)
			}
		})
	}
}
