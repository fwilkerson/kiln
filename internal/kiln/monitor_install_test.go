package kiln

import (
	"strings"
	"testing"
)

// TestLaunchdJobEnv holds the variables a launchd job runs kiln monitor run with. launchd gives a job neither, so
// the job keeps the installer's PATH, and its TMPDIR when it has one.
func TestLaunchdJobEnv(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want string
	}{
		{"a TMPDIR is kept", []string{"PATH=/usr/bin", "TMPDIR=/Volumes/quiet/tmp"},
			"    <key>PATH</key>\n    <string>/usr/bin</string>\n" +
				"    <key>TMPDIR</key>\n    <string>/Volumes/quiet/tmp</string>\n  </dict>\n"},
		{"no TMPDIR, none written", []string{"PATH=/usr/bin"},
			"    <key>PATH</key>\n    <string>/usr/bin</string>\n  </dict>\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Repo{top: "/repo", exe: "/bin/kiln", store: store{dir: "/repo/.git/kiln"}, env: tt.env}
			if job := string(r.launchdJob("dev.kiln.monitor.x", 24)); !strings.Contains(job, tt.want) {
				t.Errorf("the job's environment is not\n%s\nin\n%s", tt.want, job)
			}
		})
	}
}
