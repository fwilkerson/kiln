package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHelp holds kiln help's wiring. It prints the usage, then the topics, or one topic's part of kiln's docs. It
// refuses a topic the docs lack as a usage error, and kiln complete help offers the topics.
func TestHelp(t *testing.T) {
	for _, tc := range []struct {
		args        []string
		code        int
		out, stderr string
	}{
		{args: []string{"help"}, out: "\ntopics, for kiln help <topic>:\n  all specs files presets agents "},
		{args: []string{"help", "gate"}, out: "# The gate\n"},
		{args: []string{"help", "gates"}, code: 2, stderr: "kiln help: usage: kiln help has no topic gates, only all"},
		{args: []string{"complete", "help"}, out: "all\nspecs\nfiles\npresets\n"},
	} {
		dir := t.TempDir()
		stdout, err := os.Create(filepath.Join(dir, "stdout"))
		if err != nil {
			t.Fatal(err)
		}

		stderr, err := os.Create(filepath.Join(dir, "stderr"))
		if err != nil {
			t.Fatal(err)
		}

		code := run(tc.args, strings.NewReader(""), stdout, stderr)
		out, _ := os.ReadFile(stdout.Name())
		said, _ := os.ReadFile(stderr.Name())
		got := fmt.Sprint(code, strings.Contains(string(out), tc.out), strings.Contains(string(said), tc.stderr))
		if got != fmt.Sprint(tc.code, true, true) {
			t.Errorf("kiln %s exits %d, and prints:\n%s\n%s\nwant %d, %q and %q", strings.Join(tc.args, " "), code,
				out, said, tc.code, tc.out, tc.stderr)
		}
	}
}
