package pythonpath

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-python/internal/testing/fakepyenv"
)

func TestLink_RegistersWithPyenv(t *testing.T) {
	install := fakeInstall(t, "")
	root := fakepyenv.New(t, "3.12.0")

	res, err := Link(Options{Root: install, Version: "3.13.1", Path: filepath.Join(root, "shims")})
	assert.NilError(t, err)
	assert.DeepEqual(t, res, Result{Dir: filepath.Join(root, "shims"), Root: install, Pyenv: "3.13.1"})

	target, err := os.Readlink(filepath.Join(root, "versions", "3.13.1"))
	assert.NilError(t, err)
	assert.Equal(t, target, install)
	global, err := os.ReadFile(filepath.Join(root, "version"))
	assert.NilError(t, err)
	assert.Equal(t, string(global), "3.13.1\n")
	_, err = os.Stat(filepath.Join(root, "rehashed"))
	assert.NilError(t, err, "expected pyenv to rehash")
}

func TestLink_RegistersWithPyenvBesideItsOwnInstall(t *testing.T) {
	install := fakeInstall(t, "")
	root := fakepyenv.New(t, "3.13.1")

	res, err := Link(Options{Root: install, Version: "3.13.1", Path: filepath.Join(root, "shims")})
	assert.NilError(t, err)
	assert.Equal(t, res.Pyenv, "3.13.1-setup-python")
	_, err = os.Stat(filepath.Join(root, "versions", "3.13.1", "bin", "python"))
	assert.NilError(t, err, "pyenv's own install was disturbed")
}

func TestLink_RerunReplacesItsPyenvLink(t *testing.T) {
	root := fakepyenv.New(t, "3.12.0")
	for range 2 {
		install := fakeInstall(t, "")
		res, err := Link(Options{Root: install, Version: "3.13.1", Path: filepath.Join(root, "shims")})
		assert.NilError(t, err)
		assert.Equal(t, res.Pyenv, "3.13.1")
		target, err := os.Readlink(filepath.Join(root, "versions", "3.13.1"))
		assert.NilError(t, err)
		assert.Equal(t, target, install)
	}
}

func TestLink_PyenvVersionOverridesGlobal(t *testing.T) {
	install := fakeInstall(t, "")
	root := fakepyenv.New(t, "3.12.0")
	t.Setenv("PYENV_VERSION", "3.12.0")

	_, err := Link(Options{Root: install, Version: "3.13.1", Path: filepath.Join(root, "shims")})
	assert.ErrorContains(t, err, `pyenv runs 3.12.0 (set by `+root+`/version) here, whose python reports "Python 3.12.0" rather than Python 3.13.1`)
}

// TestLink_AcceptsPyenvsOwnInstallOfTheVersion covers a .python-version naming
// a version pyenv already has, as on an image that ships it: pyenv runs its
// own install rather than setup-python's, which is just as good.
func TestLink_AcceptsPyenvsOwnInstallOfTheVersion(t *testing.T) {
	install := fakeInstall(t, "")
	root := fakepyenv.New(t, "3.13.1")
	t.Setenv("PYENV_VERSION", "3.13.1")

	res, err := Link(Options{Root: install, Version: "3.13.1", Path: filepath.Join(root, "shims")})
	assert.NilError(t, err)
	assert.DeepEqual(t, res, Result{Dir: filepath.Join(root, "shims"), Root: filepath.Join(root, "versions", "3.13.1"), Pyenv: "3.13.1-setup-python"})
}

func TestLink_RejectsPyenvsOwnInstallBuiltDifferently(t *testing.T) {
	install := fakeInstall(t, "")
	root := fakepyenv.New(t, "3.13.1")
	assert.NilError(t, os.WriteFile(filepath.Join(root, "versions", "3.13.1", "bin", "python3.13t"), nil, 0600))
	t.Setenv("PYENV_VERSION", "3.13.1")

	_, err := Link(Options{Root: install, Version: "3.13.1", Path: filepath.Join(root, "shims")})
	assert.ErrorContains(t, err, "pyenv runs 3.13.1 (set by "+root+"/version) here, which is free-threaded")
}

func TestPyenvRoot(t *testing.T) {
	root := fakepyenv.New(t, "3.12.0")
	got, ok := pyenvRoot(filepath.Join(root, "shims"))
	assert.Assert(t, ok)
	assert.Equal(t, got, root)

	other := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(other, "python"), []byte("#!/bin/sh\n"), 0700)) //nolint:gosec // must be executable
	_, ok = pyenvRoot(other + string(os.PathListSeparator) + filepath.Join(root, "shims"))
	assert.Assert(t, !ok, "a python ahead of pyenv's shims is not pyenv's")

	_, ok = pyenvRoot(t.TempDir())
	assert.Assert(t, !ok)
}
