package kiln

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MonitorInstall sets up a launchd job that runs kiln monitor run in the repository each hour. It writes the job to
// ~/Library/LaunchAgents/, named for a hash of the repository's path, and loads it with launchctl bootstrap. The job
// runs at the minute of the hour it was installed at. launchd runs such a job on wake when its minute passed during
// sleep. The job runs with the PATH of the kiln monitor install that set it up. It keeps that command's TMPDIR
// too, when it has one. It writes what it prints to monitor.log, beside kiln's ledger. Installed again, it replaces
// the job.
func (r *Repo) MonitorInstall(ctx context.Context, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	label := "dev.kiln.monitor." + hashOf(r.top)
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return err
	}

	minute := time.Now().Minute()
	job := r.launchdJob(label, minute)
	if err := os.WriteFile(plist, job, 0o644); err != nil {
		return err
	}

	domain := fmt.Sprintf("gui/%d", os.Getuid())
	// A job installed before is booted out first. One that isn't loaded makes bootout fail, which changes nothing.
	_ = exec.CommandContext(ctx, "launchctl", "bootout", domain+"/"+label).Run()
	if b, err := exec.CommandContext(ctx, "launchctl", "bootstrap", domain, plist).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap %s %s: %w: %s", domain, plist, err, strings.TrimSpace(string(b)))
	}

	fmt.Fprintf(out, "installed %s, which runs kiln monitor run in %s at minute %d of each hour\n", plist, r.top,
		minute)
	return nil
}

// launchdJob is the property list of the launchd job named label, which runs kiln monitor run at minute.
func (r *Repo) launchdJob(label string, minute int) []byte {
	log := filepath.Join(r.store.dir, "monitor.log")
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" ` +
		`"http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n<plist version=\"1.0\">\n<dict>\n")
	key := func(k, v string) { fmt.Fprintf(&b, "  <key>%s</key>\n  <string>%s</string>\n", k, escaped(v)) }
	key("Label", label)
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, arg := range []string{r.exe, "monitor", "run"} {
		fmt.Fprintf(&b, "    <string>%s</string>\n", escaped(arg))
	}

	b.WriteString("  </array>\n")
	key("WorkingDirectory", r.top)
	b.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
	for _, name := range jobEnv {
		if value := envValue(r.env, name); value != "" {
			fmt.Fprintf(&b, "    <key>%s</key>\n    <string>%s</string>\n", name, escaped(value))
		}
	}

	b.WriteString("  </dict>\n")
	fmt.Fprintf(&b, "  <key>StartCalendarInterval</key>\n  <dict>\n    <key>Minute</key>\n    <integer>%d</integer>\n"+
		"  </dict>\n", minute)
	key("StandardOutPath", log)
	key("StandardErrorPath", log)
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}

// jobEnv names the variables the launchd job keeps from kiln monitor install's environment. launchd gives a job
// neither. TMPDIR is where the gate's tests write, which a developer may keep on a volume Spotlight never reads.
var jobEnv = []string{"PATH", "TMPDIR"}

// escaped is s, escaped for XML's character data.
func escaped(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
