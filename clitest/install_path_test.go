package clitest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk/sdktest"

	"github.com/circleci-functions/setup-python/internal/testing/fakepyenv"
)

// These tests run without --bin-dir, on a PATH of temporary directories, so
// setup-python finds a directory on PATH to link into and checks what PATH
// then resolves, as it does in a job.

// userBinDir creates the sandbox's ~/.local/bin, the directory setup-python
// prefers to link into when PATH holds it.
func userBinDir(t *testing.T, s *sdktest.Sandbox) string {
	t.Helper()
	dir := filepath.Join(s.Home, ".local", "bin")
	assert.NilError(t, os.MkdirAll(dir, 0750))
	return dir
}

func TestCLI_Install_LinksOntoPath(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{name: "build with bin/python", version: "3.13.1"},
		{name: "build without bin/python", version: "3.10.12"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := installEnv(t)
			s := sdktest.NewSandbox(t)
			userBin := userBinDir(t, s)

			out, err := runIn(t, s, t.TempDir(), map[string]string{"PATH": userBin},
				"--downloads-url", srv.MetadataURL(), "--python-version", tc.version)
			assert.NilError(t, err, "output: %s", out)
			assert.Equal(t, pythonVersion(t, userBin), "Python "+tc.version)
			assert.Assert(t, strings.Contains(out, "linked into "+userBin), "output: %s", out)
			assert.Assert(t, strings.Contains(out, "pip installs packages for the user"), "output: %s", out)
		})
	}
}

// TestCLI_Install_TakesOverAPipAhead covers another pip coming ahead of the
// directory python is linked into, as a preinstalled Python's Scripts
// directory does on Windows.
func TestCLI_Install_TakesOverAPipAhead(t *testing.T) {
	srv, _ := installEnv(t)
	s := sdktest.NewSandbox(t)
	userBin := userBinDir(t, s)
	otherBin := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(otherBin, "pip"), []byte("#!/bin/sh\n"), 0700)) //nolint:gosec // must be executable

	out, err := runIn(t, s, t.TempDir(), map[string]string{"PATH": otherBin + string(os.PathListSeparator) + userBin},
		"--downloads-url", srv.MetadataURL(), "--python-version", "3.13.1")
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "also linked pip into "+otherBin), "output: %s", out)
	assert.Assert(t, !strings.Contains(out, "warning: pip resolves from"), "output: %s", out)
	target, err := os.Readlink(filepath.Join(otherBin, "pip"))
	assert.NilError(t, err)
	assert.Equal(t, target, filepath.Join(s.Home, ".cache", "setup-python", "3.13.1-"+host(t).String(), "bin", "pip"))
}

func TestCLI_Install_RegistersWithPyenv(t *testing.T) {
	srv, _ := installEnv(t)
	s := sdktest.NewSandbox(t)
	root := fakepyenv.New(t, "3.12.0")

	out, err := runIn(t, s, t.TempDir(), map[string]string{"PATH": filepath.Join(root, "shims")},
		"--downloads-url", srv.MetadataURL(), "--python-version", "3.13.1")
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "registered Python 3.13.1 with pyenv as 3.13.1,"), "output: %s", out)
	target, err := os.Readlink(filepath.Join(root, "versions", "3.13.1"))
	assert.NilError(t, err)
	assert.Equal(t, target, filepath.Join(s.Home, ".cache", "setup-python", "3.13.1-"+host(t).String()))
	global, err := os.ReadFile(filepath.Join(root, "version"))
	assert.NilError(t, err)
	assert.Equal(t, string(global), "3.13.1\n")
}

// TestCLI_Install_UsesPyenvsOwnInstallOfTheVersion covers an image whose pyenv
// already has the version asked for, selected ahead of pyenv's global version:
// pyenv keeps running its own install, and --pip-version applies to that.
func TestCLI_Install_UsesPyenvsOwnInstallOfTheVersion(t *testing.T) {
	srv, _ := installEnv(t)
	s := sdktest.NewSandbox(t)
	root := fakepyenv.New(t, "3.13.1")
	log := filepath.Join(t.TempDir(), "stub.log")

	out, err := runIn(t, s, t.TempDir(), map[string]string{"PATH": filepath.Join(root, "shims"), "PYENV_VERSION": "3.13.1", "STUB_LOG": log},
		"--downloads-url", srv.MetadataURL(), "--python-version", "3.13.1", "--pip-version", "24.2")
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "registered Python 3.13.1 with pyenv as 3.13.1-setup-python,"), "output: %s", out)
	assert.Assert(t, strings.Contains(out, "pyenv runs its own Python 3.13.1 from "+filepath.Join(root, "versions", "3.13.1")), "output: %s", out)
	assert.DeepEqual(t, stubCalls(t, log), []string{"pyenv python -m pip install --no-user --disable-pip-version-check pip==24.2"})
}
