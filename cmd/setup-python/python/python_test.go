package python_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/circleci/functions/cmd/setup-python/python"

	"gotest.tools/v3/assert"
)

// writeFakeExecutable creates an empty, executable file named name inside
// dir, standing in for a real python/pip/python3/pip3 binary for
// exec.LookPath purposes.
func writeFakeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	assert.NilError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755))
}

func TestPythonBinary_FallsBackWhenPythonMissing(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "python3")
	writeFakeExecutable(t, dir, "pip3")
	t.Setenv("PATH", dir)

	got := python.ResolveBinaries()

	assert.Equal(t, got.Python, "python3")
	assert.Equal(t, got.Pip, "pip3")
	assert.Equal(t, got.NeedsAlias, true)
}

func TestPythonBinary_PrefersPythonWhenPresent(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "python")
	writeFakeExecutable(t, dir, "pip")
	t.Setenv("PATH", dir)

	got := python.ResolveBinaries()

	assert.Equal(t, got.Python, "python")
	assert.Equal(t, got.Pip, "pip")
	assert.Equal(t, got.NeedsAlias, false)
}

func TestAlias_WrittenWhenNeeded(t *testing.T) {
	bashEnvPath := filepath.Join(t.TempDir(), "env.sh")
	t.Setenv("BASH_ENV", bashEnvPath)

	assert.NilError(t, python.EnsureAliased(python.Binaries{
		Python:     "python3",
		Pip:        "pip3",
		NeedsAlias: true,
	}))

	got, err := os.ReadFile(bashEnvPath)
	assert.NilError(t, err)
	assert.Equal(t, string(got), "shopt -s expand_aliases\nalias pip=\"pip3\"\nalias python=\"python3\"\n")
}

func TestAlias_NotWrittenWhenNotNeeded(t *testing.T) {
	bashEnvPath := filepath.Join(t.TempDir(), "env.sh")
	t.Setenv("BASH_ENV", bashEnvPath)

	assert.NilError(t, python.EnsureAliased(python.Binaries{
		Python:     "python",
		Pip:        "pip",
		NeedsAlias: false,
	}))

	_, err := os.ReadFile(bashEnvPath)
	assert.Assert(t, os.IsNotExist(err), "BASH_ENV must be untouched when no alias is needed")
}
