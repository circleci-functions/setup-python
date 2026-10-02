package pythonpath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

// fakePyenv writes a pyenv root whose shims run the version named by
// $PYENV_VERSION or the root's version file, as pyenv's do, holding a version
// of its own named preinstalled.
func fakePyenv(t *testing.T, preinstalled string) string {
	t.Helper()
	skip.If(t, runtime.GOOS == "windows", "the fake pyenv is shell scripts")
	root := t.TempDir()
	for _, dir := range []string{"bin", "shims", filepath.Join("versions", preinstalled, "bin")} {
		assert.NilError(t, os.MkdirAll(filepath.Join(root, dir), 0750))
	}
	assert.NilError(t, os.WriteFile(filepath.Join(root, "version"), []byte(preinstalled+"\n"), 0600))
	selected := `v=${PYENV_VERSION:-$(/bin/cat "` + root + `/version")}`
	scripts := map[string]string{
		"bin/pyenv": "#!/bin/sh\n" + selected + "\ncase \"$1\" in\n" +
			"global) printf '%s\\n' \"$2\" > \"" + root + "/version\" ;;\n" +
			"rehash) : > \"" + root + "/rehashed\" ;;\n" +
			"which) echo \"" + root + "/versions/$v/bin/$2\" ;;\n" +
			"prefix) echo \"" + root + "/versions/$v\" ;;\n" +
			"version) echo \"$v (set by " + root + "/version)\" ;;\n" +
			"esac\n",
		"shims/python": "#!/bin/sh\n" + selected + "\nexec \"" + root + "/versions/$v/bin/python\" \"$@\"\n",
		filepath.Join("versions", preinstalled, "bin", "python"): "#!/bin/sh\necho 'Python " + preinstalled + "'\n",
	}
	for name, content := range scripts {
		assert.NilError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0700)) //nolint:gosec // must be executable
	}
	return root
}

func TestLink_RegistersWithPyenv(t *testing.T) {
	install := fakeInstall(t, "")
	root := fakePyenv(t, "3.12.0")

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
	root := fakePyenv(t, "3.13.1")

	res, err := Link(Options{Root: install, Version: "3.13.1", Path: filepath.Join(root, "shims")})
	assert.NilError(t, err)
	assert.Equal(t, res.Pyenv, "3.13.1-setup-python")
	_, err = os.Stat(filepath.Join(root, "versions", "3.13.1", "bin", "python"))
	assert.NilError(t, err, "pyenv's own install was disturbed")
}

func TestLink_RerunReplacesItsPyenvLink(t *testing.T) {
	root := fakePyenv(t, "3.12.0")
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
	root := fakePyenv(t, "3.12.0")
	t.Setenv("PYENV_VERSION", "3.12.0")

	_, err := Link(Options{Root: install, Version: "3.13.1", Path: filepath.Join(root, "shims")})
	assert.ErrorContains(t, err, `pyenv runs 3.12.0 (set by `+root+`/version) here, whose python reports "Python 3.12.0" rather than Python 3.13.1`)
}

// TestLink_AcceptsPyenvsOwnInstallOfTheVersion covers a .python-version naming
// a version pyenv already has, as on an image that ships it: pyenv runs its
// own install rather than setup-python's, which is just as good.
func TestLink_AcceptsPyenvsOwnInstallOfTheVersion(t *testing.T) {
	install := fakeInstall(t, "")
	root := fakePyenv(t, "3.13.1")
	t.Setenv("PYENV_VERSION", "3.13.1")

	res, err := Link(Options{Root: install, Version: "3.13.1", Path: filepath.Join(root, "shims")})
	assert.NilError(t, err)
	assert.DeepEqual(t, res, Result{Dir: filepath.Join(root, "shims"), Root: filepath.Join(root, "versions", "3.13.1"), Pyenv: "3.13.1-setup-python"})
}

func TestLink_RejectsPyenvsOwnInstallBuiltDifferently(t *testing.T) {
	install := fakeInstall(t, "")
	root := fakePyenv(t, "3.13.1")
	assert.NilError(t, os.WriteFile(filepath.Join(root, "versions", "3.13.1", "bin", "python3.13t"), nil, 0600))
	t.Setenv("PYENV_VERSION", "3.13.1")

	_, err := Link(Options{Root: install, Version: "3.13.1", Path: filepath.Join(root, "shims")})
	assert.ErrorContains(t, err, "pyenv runs 3.13.1 (set by "+root+"/version) here, which is free-threaded")
}

func TestPyenvRoot(t *testing.T) {
	root := fakePyenv(t, "3.12.0")
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
