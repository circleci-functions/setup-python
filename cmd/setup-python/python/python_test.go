package python_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/functions/cmd/setup-python/python"
)

// writeFakeExecutable creates an empty, executable file named name inside dir,
// standing in for a real python/pip/python3/pip3 binary for exec.LookPath
// purposes. On Windows the name gets a .exe suffix, since LookPath only
// considers files with a $PATHEXT extension.
func writeFakeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	assert.NilError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755))
}

// TestResolveBinaries verifies python and pip are preferred when python is on
// PATH, and that python3/pip3 are used with NeedsAlias set when it is not.
func TestResolveBinaries(t *testing.T) {
	tests := []struct {
		name   string
		onPath []string
		want   python.Binaries
	}{
		{
			name:   "python on PATH",
			onPath: []string{"python", "pip"},
			want:   python.Binaries{Python: "python", Pip: "pip", NeedsAlias: false},
		},
		{
			name:   "only python3 on PATH",
			onPath: []string{"python3", "pip3"},
			want:   python.Binaries{Python: "python3", Pip: "pip3", NeedsAlias: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tc.onPath {
				writeFakeExecutable(t, dir, name)
			}
			t.Setenv("PATH", dir)

			assert.DeepEqual(t, python.ResolveBinaries(), tc.want)
		})
	}
}

// TestEnsureAliased verifies the python/pip alias is appended to $BASH_ENV only
// when the resolved binaries need one; otherwise $BASH_ENV is left untouched.
func TestEnsureAliased(t *testing.T) {
	tests := []struct {
		name     string
		binaries python.Binaries
		want     string // empty means $BASH_ENV must not be written at all
	}{
		{
			name:     "alias needed",
			binaries: python.Binaries{Python: "python3", Pip: "pip3", NeedsAlias: true},
			want:     "shopt -s expand_aliases\nalias pip=\"pip3\"\nalias python=\"python3\"\n",
		},
		{
			name:     "alias not needed",
			binaries: python.Binaries{Python: "python", Pip: "pip", NeedsAlias: false},
			want:     "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bashEnvPath := filepath.Join(t.TempDir(), "env.sh")
			t.Setenv("BASH_ENV", bashEnvPath)

			assert.NilError(t, python.EnsureAliased(tc.binaries))

			got, err := os.ReadFile(bashEnvPath)
			if tc.want == "" {
				assert.Assert(t, os.IsNotExist(err), "BASH_ENV must be untouched when no alias is needed")
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, string(got), tc.want)
		})
	}
}
