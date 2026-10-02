// Package pythonpath makes an unpacked Python install usable by name for the
// rest of a CircleCI job.
package pythonpath

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/circleci-functions/toolkit/pathlink"
)

// Options configures Link.
type Options struct {
	// Root is the unpacked install to make resolvable, as pythondist.Install
	// left it.
	Root string
	// Version is what python --version must report, e.g. "3.13.1".
	Version string
	// BinDir links into exactly this directory, skipping PATH discovery.
	BinDir string
	// Path is the search path to use. Empty means this process's PATH.
	Path string
}

// BinDir returns the directory of an install at root holding python: bin on
// Unix, the root itself on Windows.
func BinDir(root string) string {
	if runtime.GOOS == "windows" {
		return root
	}
	return filepath.Join(root, "bin")
}

// ScriptsDir returns where pip puts the executables of the packages it
// installs into the install at root, unless told to install for the user.
func ScriptsDir(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "Scripts")
	}
	return filepath.Join(root, "bin")
}

// Executable returns the python executable of the install at root.
func Executable(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "python.exe")
	}
	return filepath.Join(root, "bin", "python3")
}

// Installed reports whether root holds a Python install.
func Installed(root string) bool {
	_, err := os.Stat(Executable(root))
	return err == nil
}

// Result describes where Link made python and pip resolvable from.
type Result struct {
	// Dir is the directory python resolves from.
	Dir string
	// Root is the root of the install python runs: Options.Root, unless a
	// .python-version file or $PYENV_VERSION has pyenv run its own install of
	// the same version.
	Root string
	// Pyenv is the name the install was registered with pyenv as, when python
	// was already on PATH through pyenv's shims, and "" otherwise.
	Pyenv string
	// PipDirs are further directories pip was linked into, taking each over,
	// because another pip there came ahead of Dir on PATH.
	PipDirs []string
}

// Link makes python, python3, python3.X, pip, pip3 and pip3.X resolvable by
// name from a directory already on PATH.
//
// Where python is already on PATH through pyenv's shims, it registers the
// install with pyenv and makes it pyenv's global version instead, since pyenv
// rewrites its shims directory and would take any link there with it.
func Link(opts Options) (Result, error) {
	minor, err := minorVersion(opts.Version)
	if err != nil {
		return Result{}, err
	}
	if opts.BinDir == "" {
		if root, ok := pyenvRoot(opts.Path); ok {
			return linkWithPyenv(root, opts)
		}
	}

	from := BinDir(opts.Root)
	pips := []string{"pip", "pip3", "pip" + minor}
	lopts := pathlink.Options{
		FromDir:   from,
		Preferred: pathlink.UserBinDirs(),
		Dir:       opts.BinDir,
		Path:      opts.Path,
		Verify:    verify(from, opts.Version),
	}
	if runtime.GOOS == "windows" {
		lopts.Names = []string{"python"}
		lopts.Aliases = map[string]string{"python3": "python", "python" + minor: "python"}
	} else {
		lopts.Names = present(from, []string{"python", "python3", "python" + minor})
	}
	res, err := pathlink.Ensure(lopts)
	if err != nil {
		return Result{}, err
	}

	result := Result{Dir: res.Dir, Root: opts.Root}
	if err := linkPips(opts.Root, res.Dir, pips); err != nil {
		return result, err
	}
	if opts.BinDir == "" {
		result.PipDirs = takeOverPips(opts.Root, res.Dir, opts.Path, pips)
	}
	return result, nil
}

// linkPips links the install's pip executables into dir or, on Windows,
// whose builds ship none, writes shims there that run python -m pip.
func linkPips(root, dir string, pips []string) error {
	if runtime.GOOS == "windows" {
		for _, pip := range pips {
			if err := writePipShims(dir, Executable(root), pip); err != nil {
				return err
			}
		}
		return nil
	}
	names := present(BinDir(root), pips)
	if len(names) == 0 {
		return nil
	}
	_, err := pathlink.Ensure(pathlink.Options{Names: names, FromDir: BinDir(root), Dir: dir})
	return err
}

// takeOverPips links each of pips into the directory it resolves from instead
// of dir, where another pip comes ahead of the one in dir, and returns those
// directories. On Windows that is the Scripts directory of a preinstalled
// Python, which comes ahead of the Python itself on PATH. A directory it cannot
// write to is left alone; Shadowed reports what still resolves from there.
func takeOverPips(root, dir, path string, pips []string) []string {
	var taken []string
	for _, pip := range pips {
		other := shadowed(path, dir, pip)
		if other == "" {
			continue
		}
		if runtime.GOOS == "windows" {
			if err := removeExe(other, pip); err != nil {
				continue
			}
		}
		if err := linkPips(root, other, []string{pip}); err != nil {
			continue
		}
		if !slices.Contains(taken, other) {
			taken = append(taken, other)
		}
	}
	return taken
}

// removeExe gets dir's name.exe out of the way of the shims written beside
// it, which PATHEXT would otherwise pick it ahead of. Windows refuses to
// delete an executable that is running but lets it be renamed, so it renames
// first and the delete is best effort.
func removeExe(dir, name string) error {
	exe := filepath.Join(dir, name+".exe")
	if _, err := os.Stat(exe); err != nil {
		return nil
	}
	aside := exe + ".setup-python-old"
	if err := os.Rename(exe, aside); err != nil {
		return err
	}
	_ = os.Remove(aside)
	return nil
}

// Shadowed returns the directory name resolves from on PATH when that is not
// dir, or "" when it is dir or resolves nowhere.
func Shadowed(dir, name string) string {
	return shadowed("", dir, name)
}

func shadowed(path, dir, name string) string {
	dirs := pathlink.SearchDirs(path)
	i := pathlink.IndexOf(dirs, name)
	if i < 0 || sameFile(dirs[i], dir) {
		return ""
	}
	return dirs[i]
}

// RemoveUserInstalls removes any setting UserInstalls wrote into the install
// at root.
func RemoveUserInstalls(root string) error {
	if err := os.Remove(pipConfig(root)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// UserInstalls makes pip install packages for the user rather than into the
// install at root, when pip's user scripts directory is linkedDir, so that the
// executables of the packages it installs resolve by name like python itself.
// It returns pip's user scripts directory.
//
// The setting goes in the install's own pip config, which pip reads for this
// interpreter only: a virtual environment made from it does not inherit it.
// When the user scripts directory is somewhere else, it removes any setting an
// earlier run left and returns an error saying so.
func UserInstalls(root, linkedDir string) (string, error) {
	out, err := exec.Command(Executable(root), "-c", "import os, sysconfig; print(sysconfig.get_path('scripts', os.name + '_user'))").Output() //nolint:gosec // the install's own python
	if err != nil {
		return "", fmt.Errorf("ask python for pip's user scripts directory: %w", err)
	}
	userScripts := strings.TrimSpace(string(out))
	conf := pipConfig(root)
	if !sameFile(userScripts, linkedDir) {
		if err := RemoveUserInstalls(root); err != nil {
			return userScripts, err
		}
		return userScripts, fmt.Errorf("pip's user scripts directory %s is not %s", userScripts, linkedDir)
	}
	//nolint:gosec // pip config is not secret
	if err := os.WriteFile(conf, []byte("[install]\nuser = true\n"), 0644); err != nil {
		return userScripts, fmt.Errorf("write %s: %w", conf, err)
	}
	return userScripts, nil
}

// pipConfig returns the site-wide pip config file of the install at root.
func pipConfig(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "pip.ini")
	}
	return filepath.Join(root, "pip.conf")
}

// verify accepts only a python reporting version and, on Unix, resolving to
// this install's own, so a preinstalled python that happens to answer the same
// does not pass for it. On Windows the links are shims that call python.exe by
// absolute path, so there only the version can identify them.
func verify(from, version string) func(string) error {
	want := filepath.Join(from, "python")
	return func(bin string) error {
		if err := reportsVersion(bin, version); err != nil {
			return err
		}
		if runtime.GOOS != "windows" && !sameFile(bin, want) {
			return fmt.Errorf("is not %s", want)
		}
		return nil
	}
}

// reportsVersion checks the python at bin runs and reports version.
func reportsVersion(bin, version string) error {
	out, err := pathlink.Command(bin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if got := strings.TrimSpace(string(out)); got != "Python "+version {
		return fmt.Errorf("reports %q rather than Python %s", got, version)
	}
	return nil
}

// writePipShims makes pip from a Windows install resolvable from dir, in cmd,
// PowerShell and Git Bash. Windows builds ship no pip executables, only the
// pip module, so the shims run it with python -m.
func writePipShims(dir, python, name string) error {
	shims := map[string]string{
		name + ".bat": fmt.Sprintf("@echo off\r\n\"%s\" -m pip %%*\r\n", python),
		name:          fmt.Sprintf("#!/bin/sh\nexec \"%s\" -m pip \"$@\"\n", filepath.ToSlash(python)),
	}
	for file, content := range shims {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0755); err != nil { //nolint:gosec // must be executable
			return err
		}
	}
	return nil
}

// minorVersion returns the "3.X" of a version such as "3.13.1".
func minorVersion(version string) (string, error) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid Python version %q", version)
	}
	return parts[0] + "." + parts[1], nil
}

// present returns the names that exist in dir.
func present(dir string, names []string) []string {
	var out []string
	for _, name := range names {
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			out = append(out, name)
		}
	}
	return out
}

func sameFile(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ra, rb)
	}
	return ra == rb
}
