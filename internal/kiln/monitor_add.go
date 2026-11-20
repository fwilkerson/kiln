package kiln

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// shipped holds the monitors kiln ships, each a directory as .kiln/monitors/ holds one. A monitor that needs one
// language's tools sits under that language's preset, such as go/docs.
//
//go:embed monitors
var shipped embed.FS

// shippedDir is the directory of shipped that holds the monitors.
const shippedDir = "monitors"

// MonitorAdd copies a monitor into .kiln/monitors/<name>/ in the checkout, for the developer to commit on main. source
// names a monitor kiln ships, such as bugs. One that needs a language's tools sits under its preset's name, such as
// go/docs. A source that starts with a dot or a slash is a directory. The monitor's name is source's last element.
// Before it writes, it refuses a monitor kiln could not run, and a name already there.
func (r *Repo) MonitorAdd(source string, out io.Writer) error {
	from, err := monitorSource(source)
	if err != nil {
		return err
	}

	m, files, err := r.addable(from, Spec(filepath.Base(source)))
	if err != nil {
		return err
	}

	for _, f := range files {
		raw, err := fs.ReadFile(from, f)
		if err != nil {
			return err
		}

		target := filepath.Join(r.top, m.path(f))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		if err := os.WriteFile(target, raw, 0o644); err != nil {
			return err
		}

		fmt.Fprintf(out, "wrote %s\n", m.path(f))
	}

	fmt.Fprintf(out, "commit %s/ on main: kiln reads it there\n", m.path(""))
	return nil
}

// monitorSource is the directory source names: a monitor kiln ships, or a path starting with a dot or a slash. It
// refuses a name kiln ships no monitor of, and a path to no directory.
func monitorSource(source string) (fs.FS, error) {
	if strings.HasPrefix(source, ".") || filepath.IsAbs(source) {
		if info, err := os.Stat(source); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("%w: %s is no directory", ErrUsage, source)
		}

		return os.DirFS(source), nil
	}

	names := shippedNames()
	if !slices.Contains(names, source) {
		return nil, fmt.Errorf("%w: kiln ships no monitor %q: name one of %s, or a directory that starts with . or /",
			ErrUsage, source, strings.Join(names, ", "))
	}

	return fs.Sub(shipped, path.Join(shippedDir, source))
}

// shippedNames are the monitors kiln ships, by their paths under shippedDir: each directory that holds a file.
func shippedNames() []string {
	var names []string
	_ = fs.WalkDir(shipped, shippedDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			names = append(names, strings.TrimPrefix(path.Dir(p), shippedDir+"/"))
		}

		return err
	})

	// WalkDir walks in lexical order, so the files of one directory come together.
	return slices.Compact(names)
}

// addable reads the monitor name from the directory from, and answers it with the files the directory holds. It
// refuses a monitor kiln could not run, by the checkout's kiln.json, and a name the checkout's .kiln/monitors/ holds.
func (r *Repo) addable(from fs.FS, name Spec) (monitor, []string, error) {
	c, err := r.checkoutConfig()
	if err != nil {
		return monitor{}, nil, err
	}

	files, settings, err := monitorFiles(from)
	if err != nil {
		return monitor{}, nil, err
	}

	m, err := c.readMonitor(name, files, settings)
	switch _, there := os.Lstat(filepath.Join(r.top, m.path(""))); {
	case err != nil:
		return m, nil, fmt.Errorf("%w: %w", errRefused, err)
	case !errors.Is(there, fs.ErrNotExist):
		return m, nil, fmt.Errorf("%w: %s/ is there already, and kiln writes over no monitor", errRefused, m.path(""))
	}

	return m, files, nil
}

// monitorFiles lists each file the monitor's directory from holds, and reads its monitor.json, or nil where it holds
// none.
func monitorFiles(from fs.FS) (files []string, settings []byte, err error) {
	err = fs.WalkDir(from, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}

		return err
	})
	if err != nil || !slices.Contains(files, settingsFile) {
		return files, nil, err
	}

	settings, err = fs.ReadFile(from, settingsFile)
	return files, settings, err
}
