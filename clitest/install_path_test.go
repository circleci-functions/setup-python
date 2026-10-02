package clitest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
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
