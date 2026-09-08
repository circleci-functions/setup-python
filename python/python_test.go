// Internal test package (not python_test) so tests can drive the linkAliases
// seam, which links into a temp directory rather than the real PATH.
package python

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// writeFakeExecutable creates an empty, executable file named name inside dir,
// standing in for a real python/pip/python3/pip3 binary for exec.LookPath
// purposes. On Windows the name gets a .exe suffix, since LookPath only
// considers files with a $PATHEXT extension.
func writeFakeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	assert.NilError(t, os.WriteFile(filepath.Join(dir, exeName(name)), []byte("#!/bin/sh\n"), 0o755))
}

// writeFakePython creates an executable named name inside dir that reports
// version, so pathlink's Verify has something to ask. Shell scripts are not
// executable on Windows, so callers skip there.
func writeFakePython(t *testing.T, dir, name, version string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	assert.NilError(t, os.WriteFile(path, []byte("#!/bin/sh\necho '"+version+"'\n"), 0o755))
	return path
}

// exeName mirrors what pathlink links under on each platform.
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// assertRuns checks that dir/name runs want: a symlink everywhere but Windows,
// where pathlink writes shims that invoke the target by absolute path.
func assertRuns(t *testing.T, dir, name, want string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		shim, err := os.ReadFile(filepath.Join(dir, name+".bat"))
		assert.NilError(t, err)
		assert.Assert(t, strings.Contains(string(shim), want), "%s must invoke %s, got %s", name, want, shim)
		return
	}
	target, err := os.Readlink(filepath.Join(dir, name))
	assert.NilError(t, err)
	assert.Equal(t, target, want)
}

// TestResolveBinaries verifies python and pip are preferred when python is on
// PATH, and that python3/pip3 are used with NeedsAlias set when it is not.
func TestResolveBinaries(t *testing.T) {
	tests := []struct {
		name   string
		onPath []string
		want   Binaries
	}{
		{
			name:   "python on PATH",
			onPath: []string{"python", "pip"},
			want:   Binaries{Python: "python", Pip: "pip", NeedsAlias: false},
		},
		{
			name:   "only python3 on PATH",
			onPath: []string{"python3", "pip3"},
			want:   Binaries{Python: "python3", Pip: "pip3", NeedsAlias: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tc.onPath {
				writeFakeExecutable(t, dir, name)
			}
			t.Setenv("PATH", dir)

			assert.DeepEqual(t, ResolveBinaries(), tc.want)
		})
	}
}

// TestEnsureAliased verifies python and pip are linked to python3 and pip3
// only when the resolved binaries need it; otherwise nothing is linked. A link
// rather than a $BASH_ENV alias, so a later step resolves them in any shell.
func TestEnsureAliased(t *testing.T) {
	tests := []struct {
		name     string
		binaries Binaries
		onPath   []string
		wantLink map[string]string // linked name -> name in FromDir it runs
	}{
		{
			name:     "alias needed",
			binaries: Binaries{Python: "python3", Pip: "pip3", NeedsAlias: true},
			onPath:   []string{"python3", "pip3"},
			wantLink: map[string]string{
				"python3": "python3",
				"pip3":    "pip3",
				"python":  "python3",
				"pip":     "pip3",
			},
		},
		{
			name:     "alias not needed",
			binaries: Binaries{Python: "python", Pip: "pip", NeedsAlias: false},
			onPath:   []string{"python", "pip"},
		},
		{
			// No python at all: the install step reports that, so there is
			// nothing to link and nothing to fail on here.
			name:     "no python3 to link",
			binaries: Binaries{Python: "python3", Pip: "pip3", NeedsAlias: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fromDir, binDir := t.TempDir(), t.TempDir()
			for _, name := range tc.onPath {
				writeFakeExecutable(t, fromDir, name)
			}
			t.Setenv("PATH", fromDir)

			assert.NilError(t, linkAliases(tc.binaries, binDir, fromDir))

			if len(tc.wantLink) == 0 {
				entries, err := os.ReadDir(binDir)
				assert.NilError(t, err)
				assert.Equal(t, len(entries), 0, "nothing must be linked when no alias is needed")
				return
			}
			for name, target := range tc.wantLink {
				assertRuns(t, binDir, name, filepath.Join(fromDir, exeName(target)))
			}
		})
	}
}

// TestVerifyPython verifies the Verify hook accepts a python and rejects
// anything else resolving under the name, which is what catches a link
// pointing at a lookalike.
func TestVerifyPython(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stand-in for python is not executable on Windows")
	}
	dir := t.TempDir()

	assert.NilError(t, verifyPython(writeFakePython(t, dir, "python3", "Python 3.12.0")))

	err := verifyPython(writeFakePython(t, dir, "notpython", "Ruby 3.3.0"))
	assert.ErrorContains(t, err, "not a Python version")

	err = verifyPython(filepath.Join(dir, "missing"))
	assert.ErrorContains(t, err, "could not report its version")
}
