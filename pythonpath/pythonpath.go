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
	"strings"

	"github.com/circleci-functions/toolkit/pathlink"
)

// Options configures Link.
type Options struct {
	// Root is the root of the Python install.
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

// Link makes python, python3, python3.X, pip, pip3 and pip3.X resolvable by
// name from a directory already on PATH, and returns that directory.
func Link(opts Options) (string, error) {
	minor, err := minorVersion(opts.Version)
	if err != nil {
		return "", err
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
		lopts.Names = present(from, append([]string{"python", "python3", "python" + minor}, pips...))
	}

	res, err := pathlink.Ensure(lopts)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		for _, pip := range pips {
			if err := writePipShims(res.Dir, Executable(opts.Root), pip); err != nil {
				return "", err
			}
		}
	}
	return res.Dir, nil
}

// Shadowed returns the directory name resolves from on PATH when that is not
// dir, or "" when it is dir or resolves nowhere. Link guarantees python
// resolves from the directory it links into, but not the pips, which can sit
// in a directory of their own ahead of it.
func Shadowed(dir, name string) string {
	dirs := pathlink.SearchDirs("")
	i := pathlink.IndexOf(dirs, name)
	if i < 0 || sameFile(dirs[i], dir) {
		return ""
	}
	return dirs[i]
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
		if err := os.Remove(conf); err != nil && !errors.Is(err, os.ErrNotExist) {
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
		out, err := pathlink.Command(bin, "--version").CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to run: %w: %s", err, strings.TrimSpace(string(out)))
		}
		if got := strings.TrimSpace(string(out)); got != "Python "+version {
			return fmt.Errorf("reports %q rather than Python %s", got, version)
		}
		if runtime.GOOS != "windows" && !sameFile(bin, want) {
			return fmt.Errorf("is not %s", want)
		}
		return nil
	}
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
