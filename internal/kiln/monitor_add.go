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
// It answers ErrUsage for a name kiln ships no monitor of, and for a path to no directory. Before it writes, it
// refuses a checkout with no kiln.json, and a monitor kiln could not run by that kiln.json. It refuses an agent.md
// kiln could not read, and a name already there. It refuses a record of kiln init it cannot read. It writes each
// script kiln runs with the executable bit. It records the monitor in .kiln/init.json, with its source, where kiln
// init brings it up to date. A directory's path is recorded from the checkout's top.
func (r *Repo) MonitorAdd(source string, out io.Writer) error {
	from, err := monitorSource(source)
	if err != nil {
		return err
	}

	rec, err := r.lastInit()
	if err != nil {
		return err
	}

	recorded, err := r.sourceFromTop(source)
	if err != nil {
		return err
	}

	m, files, err := r.addable(from, source)
	if err != nil {
		return err
	}

	written := map[string][]byte{}
	for _, f := range files {
		if written[f], err = r.writeAdded(from, m, f); err != nil {
			return err
		}

		fmt.Fprintf(out, "wrote %s\n", m.path(f))
	}

	rec.added(m.Name, addedMonitor{Source: recorded, Files: linesOf(written)})
	line, err := r.writeRecord(rec)
	if line != "" {
		fmt.Fprintln(out, line)
	}

	fmt.Fprintf(out, "commit %s/ on main: kiln reads it there\n", m.path(""))
	return err
}

// writeAdded copies file, from the monitor's directory from, into m's directory, and answers what it copied.
func (r *Repo) writeAdded(from fs.FS, m monitor, file string) ([]byte, error) {
	raw, err := fs.ReadFile(from, file)
	if err != nil {
		return nil, err
	}

	target := filepath.Join(r.top, m.path(file))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, err
	}

	return raw, os.WriteFile(target, raw, addedMode(from, file))
}

// sourceFromTop is source, as kiln monitor add takes it, as kiln init's record keeps it. A monitor kiln ships keeps
// its name. A directory's path is from the checkout's top, and starts with a dot or a slash.
func (r *Repo) sourceFromTop(source string) (string, error) {
	if !isDir(source) {
		return source, nil
	}

	abs, err := filepath.Abs(source)
	if err == nil {
		// The checkout's top has its links resolved, so a path from it needs them resolved too.
		abs, err = filepath.EvalSymlinks(abs)
	}

	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(r.top, abs)
	switch {
	case err != nil:
		return abs, nil
	case isDir(rel):
		return rel, nil
	}

	return "./" + rel, nil
}

// sourceFiles reads source, as kiln init's record keeps it, and answers its directory and each file it holds.
func (r *Repo) sourceFiles(source string) (fs.FS, map[string][]byte, error) {
	if isDir(source) && !filepath.IsAbs(source) {
		source = filepath.Join(r.top, source)
	}

	from, err := monitorSource(source)
	if err != nil {
		return nil, nil, err
	}

	names, _, err := monitorFiles(from)
	files := map[string][]byte{}
	for _, name := range names {
		if err == nil {
			files[name], err = fs.ReadFile(from, name)
		}
	}

	return from, files, err
}

// isDir is whether source, as kiln monitor add takes it, names a directory. Such a path starts with a dot or a slash.
func isDir(source string) bool {
	return strings.HasPrefix(source, ".") || filepath.IsAbs(source)
}

// ShippedMonitors prints each monitor kiln ships, one a line, with the preset it fits. One that sits under a preset's
// name, such as go/docs, fits that preset alone. Each line names the preset .kiln/init.json records, where it differs.
func (r *Repo) ShippedMonitors(out io.Writer) error {
	rec, err := r.lastInit()
	if err != nil {
		return err
	}

	for _, name := range shippedNames() {
		fmt.Fprintf(out, "%-14s %s\n", name, fitsWhat(name, rec.Preset))
	}

	fmt.Fprintln(out, "add one with kiln monitor add <name>, or one in a directory with kiln monitor add <dir>")
	return nil
}

// shippedFor is the preset the monitor kiln ships as name needs, or "" for one that fits any project.
func shippedFor(name string) string {
	lang, _, found := strings.Cut(name, "/")
	if !found {
		return ""
	}

	return lang
}

// fitsWhat says which preset the monitor kiln ships as name fits, beside preset, which a record of kiln init names.
func fitsWhat(name, preset string) string {
	lang := shippedFor(name)
	switch {
	case lang == "":
		return "for any project"
	case lang == preset:
		return fmt.Sprintf("for the preset %s, which %s records", lang, initFile)
	case slices.Contains(builtIns, preset):
		return fmt.Sprintf("for the preset %s; %s records %s", lang, initFile, preset)
	}

	return "for the preset " + lang
}

// unadded drops each of names, monitors kiln ships, whose directory the checkout's .kiln/monitors/ holds already.
func (r *Repo) unadded(names []string) []string {
	return slices.DeleteFunc(names, func(name string) bool {
		_, err := os.Lstat(filepath.Join(r.top, monitor{Name: Spec(path.Base(name))}.path("")))
		return !errors.Is(err, fs.ErrNotExist)
	})
}

// fitting are the monitors kiln ships that fit a project whose record of kiln init names preset. Where it names a
// directory, or no preset, kiln cannot tell, and each fits.
func fitting(preset string) []string {
	return slices.DeleteFunc(shippedNames(), func(name string) bool {
		lang := shippedFor(name)
		return lang != "" && slices.Contains(builtIns, preset) && lang != preset
	})
}

// addedMode is the mode kiln monitor add writes file with. A script kiln runs is executable, as is a file the source
// holds so.
func addedMode(from fs.FS, file string) fs.FileMode {
	info, err := fs.Stat(from, file)
	switch {
	case slices.Contains(roleScripts, file):
		return 0o755
	case err == nil && info.Mode()&0o111 != 0:
		return 0o755
	}

	return 0o644
}

// monitorSource is the directory source names: a monitor kiln ships, or a path starting with a dot or a slash. It
// refuses a name kiln ships no monitor of, and a path to no directory.
func monitorSource(source string) (fs.FS, error) {
	if isDir(source) {
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

// addable reads the monitor source names from the directory from, and answers it with the files the directory holds.
// It refuses a monitor kiln could not run, by the checkout's kiln.json, naming source. It refuses a name the
// checkout's .kiln/monitors/ holds.
func (r *Repo) addable(from fs.FS, source string) (monitor, []string, error) {
	dir := strings.TrimSuffix(source, "/")
	c, err := r.checkoutConfig()
	if err != nil {
		return monitor{}, nil, err
	}

	files, settings, err := monitorFiles(from)
	if err != nil {
		return monitor{}, nil, err
	}

	m, refused := c.readMonitor(Spec(path.Base(dir)), dir, files, settings)
	if m.agent {
		if err := refused.add(readableAgent(from, dir)); err != nil {
			return m, nil, err
		}
	}

	switch _, there := os.Lstat(filepath.Join(r.top, m.path(""))); {
	case len(refused) > 0:
		return m, nil, fmt.Errorf("%w: %w", errRefused, refused)
	case !errors.Is(there, fs.ErrNotExist):
		return m, nil, fmt.Errorf("%w: %s/ is there already, and kiln writes over no monitor", errRefused, m.path(""))
	}

	return m, files, nil
}

// readableAgent refuses the agent.md in the directory from, where kiln could not read it. Each refusal names the file
// in dir, the directory as kiln monitor add was given it.
func readableAgent(from fs.FS, dir string) error {
	raw, err := fs.ReadFile(from, monitorAgent)
	if err != nil {
		return err
	}

	_, err = parseAgent(string(raw))
	if refused, ok := errors.AsType[refusals](err); ok {
		return refused.about(path.Join(dir, monitorAgent) + ",")
	}

	return err
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
