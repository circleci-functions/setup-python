package pythonpath

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/circleci-functions/toolkit/pathlink"
)

// pyenvRoot returns the root of the pyenv whose shims python resolves through
// on path, if it does.
func pyenvRoot(path string) (string, bool) {
	dirs := pathlink.SearchDirs(path)
	for _, name := range []string{"python", "python3"} {
		i := pathlink.IndexOf(dirs, name)
		if i < 0 {
			continue
		}
		if filepath.Base(dirs[i]) != "shims" {
			return "", false
		}
		root := filepath.Dir(dirs[i])
		if info, err := os.Stat(filepath.Join(root, "versions")); err != nil || !info.IsDir() {
			return "", false
		}
		return root, true
	}
	return "", false
}

// linkWithPyenv registers the install with the pyenv at root, makes it
// pyenv's global version, and rehashes, so pyenv's shims run it. pyenv's own
// rehash after a pip install then puts what pip installed on PATH too.
func linkWithPyenv(root string, opts Options) (Result, error) {
	pyenv, err := pyenvCommand(root, opts.Path)
	if err != nil {
		return Result{}, err
	}
	name, err := registerWithPyenv(root, opts.Root, pyenvName(opts.Root, opts.Version))
	if err != nil {
		return Result{}, err
	}
	for _, args := range [][]string{{"global", name}, {"rehash"}} {
		if _, err := pyenv(args...); err != nil {
			return Result{}, err
		}
	}
	shims := filepath.Join(root, "shims")
	selected, err := verifyPyenv(pyenv, shims, opts.Root, opts.Version)
	if err != nil {
		return Result{}, err
	}
	return Result{Dir: shims, Root: selected, Pyenv: name}, nil
}

// pyenvName is the name to register the install with pyenv as: its version,
// with pyenv's t suffix for a free-threaded build, so a .python-version naming
// the version or its major.minor selects it.
func pyenvName(root, version string) string {
	if freethreaded(root, version) {
		return version + "t"
	}
	return version
}

// freethreaded reports whether the install at root is a free-threaded build
// of version.
func freethreaded(root, version string) bool {
	minor, _ := minorVersion(version)
	_, err := os.Lstat(filepath.Join(BinDir(root), "python"+minor+"t"))
	return err == nil
}

// registerWithPyenv links the install at install into the versions of the
// pyenv at root as name, replacing a link an earlier run left there, and
// returns the name used. A version pyenv installed itself under that name is
// left alone, and the install registered beside it instead.
func registerWithPyenv(root, install, name string) (string, error) {
	for _, candidate := range []string{name, name + "-setup-python"} {
		target := filepath.Join(root, "versions", candidate)
		info, err := os.Lstat(target)
		if err == nil && info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if err == nil {
			if err := os.Remove(target); err != nil {
				return "", fmt.Errorf("register with pyenv: %w", err)
			}
		}
		if err := os.Symlink(install, target); err != nil {
			return "", fmt.Errorf("register with pyenv: %w", err)
		}
		return candidate, nil
	}
	return "", fmt.Errorf("register with pyenv: %s already holds %s and %s-setup-python", filepath.Join(root, "versions"), name, name)
}

// verifyPyenv checks pyenv's shims run Python version, built the same way as
// the install at install, and returns the root of the install they run. A
// .python-version file or $PYENV_VERSION takes precedence over pyenv's global
// version, so either can select another install, such as one of the same
// version pyenv installed itself, which is as good as setup-python's own.
func verifyPyenv(pyenv func(...string) (string, error), shims, install, version string) (string, error) {
	selected := func() string {
		out, _ := pyenv("version")
		return strings.TrimSpace(out)
	}
	if err := reportsVersion(filepath.Join(shims, "python"), version); err != nil {
		return "", fmt.Errorf("pyenv runs %s here, whose python %w: a .python-version file or $PYENV_VERSION takes precedence over pyenv's global version",
			selected(), err)
	}
	out, err := pyenv("prefix")
	if err != nil {
		return "", err
	}
	root, _, _ := strings.Cut(strings.TrimSpace(out), string(os.PathListSeparator))
	if sameFile(root, install) {
		return install, nil
	}
	if want := freethreaded(install, version); freethreaded(root, version) != want {
		kind := "free-threaded"
		if want {
			kind = "not free-threaded"
		}
		return "", fmt.Errorf("pyenv runs %s here, which is %s: a .python-version file or $PYENV_VERSION takes precedence over pyenv's global version",
			selected(), kind)
	}
	return root, nil
}

// pyenvCommand returns a function running the pyenv at root: the pyenv on
// path, or else the one inside root.
func pyenvCommand(root, path string) (func(...string) (string, error), error) {
	bin := ""
	if i := pathlink.IndexOf(pathlink.SearchDirs(path), "pyenv"); i >= 0 {
		bin = filepath.Join(pathlink.SearchDirs(path)[i], "pyenv")
	}
	for _, candidate := range []string{filepath.Join(root, "libexec", "pyenv"), filepath.Join(root, "bin", "pyenv")} {
		if bin != "" {
			break
		}
		if _, err := os.Stat(candidate); err == nil {
			bin = candidate
		}
	}
	if bin == "" {
		return nil, fmt.Errorf("python resolves through pyenv's shims in %s, but there is no pyenv to register the install with", root)
	}
	return func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...) //nolint:gosec // the pyenv on PATH or in its root
		cmd.Env = append(os.Environ(), "PYENV_ROOT="+root)
		out, err := cmd.Output()
		if err != nil {
			var stderr string
			if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
				stderr = strings.TrimSpace(string(exitErr.Stderr))
			}
			return string(out), fmt.Errorf("pyenv %s: %w: %s", strings.Join(args, " "), err, stderr)
		}
		return string(out), nil
	}, nil
}
