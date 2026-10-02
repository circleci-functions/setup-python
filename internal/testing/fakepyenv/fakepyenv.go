// Package fakepyenv writes a stand-in for a pyenv root, so tests can register
// a Python install with pyenv and run it through pyenv's shims without a real
// pyenv.
package fakepyenv

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

// New writes a pyenv root whose shims run the version named by $PYENV_VERSION
// or the root's version file, as pyenv's do, holding a version of its own
// named preinstalled.
func New(t *testing.T, preinstalled string) string {
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
