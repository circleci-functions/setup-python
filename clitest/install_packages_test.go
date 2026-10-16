package clitest_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

// TestCLI_InstallPackages_CacheRoundTrip proves the restore -> install -> save
// contract through the compiled binary against the in-memory cache: a marker
// planted at the (overridden) venv cache path before the first run is restored
// by a second run once the path has been wiped in between. The install itself
// is a no-op stub, so this asserts only the cache-driving wiring; the real
// install is acceptance/'s job.
//
// --venv-path pins the cache to one dir this test controls (VenvPaths returns
// the override outright for any manager, including pip which otherwise caches
// no venv), and --pypi-cache=false drops the fixed per-tool pypi paths so the
// round trip is a single, fully-controlled directory.
func TestCLI_InstallPackages_CacheRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		pkgManager string
		fixture    string
		tool       string
	}{
		{name: "pip caches the overridden venv path", pkgManager: "pip", fixture: "pip"},
		{name: "poetry caches its venv", pkgManager: "poetry", fixture: "poetry", tool: "poetry"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, run := sandbox(t)
			env, _ := stubTools(t, append(pythonStubs, tc.tool)...)

			appDir := copyFixture(t, tc.fixture)
			scratch := t.TempDir()
			marker := filepath.Join(scratch, "marker")
			assert.NilError(t, os.WriteFile(marker, []byte("hit"), 0600))

			args := []string{
				"install-packages",
				"--app-dir", appDir,
				"--package-manager", tc.pkgManager,
				"--venv-path", scratch,
				"--pypi-cache=false",
			}

			out, err := run(t, env, args...)
			assert.NilError(t, err, "run 1 (save) output: %s", out)

			assert.NilError(t, os.RemoveAll(scratch))

			out, err = run(t, env, args...)
			assert.NilError(t, err, "run 2 (restore) output: %s", out)

			got, err := os.ReadFile(marker) //nolint:gosec // marker is a test temp file
			assert.NilError(t, err, "expected the marker restored from the first run's save; output: %s", out)
			assert.Equal(t, string(got), "hit")
		})
	}
}

// TestCLI_InstallPackages_CacheFlags checks which cache writes each combination
// of --cache, --pypi-cache and --venv-cache produces. Install always runs.
func TestCLI_InstallPackages_CacheFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantSaved bool
	}{
		{name: "cache enabled saves", args: []string{"--venv-path", "SCRATCH", "--pypi-cache=false"}, wantSaved: true},
		{name: "cache disabled skips the save", args: []string{"--cache=false"}, wantSaved: false},
		{name: "no cache buckets selected skips the save", args: []string{"--pypi-cache=false", "--venv-cache=false"}, wantSaved: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, run := sandbox(t)
			env, logPath := stubTools(t, pythonStubs...)
			appDir := copyFixture(t, "pip")

			args := []string{"install-packages", "--app-dir", appDir, "--package-manager", "pip"}
			for _, a := range tc.args {
				if a == "SCRATCH" {
					a = t.TempDir()
				}
				args = append(args, a)
			}

			out, err := run(t, env, args...)
			assert.NilError(t, err, "output: %s", out)

			assert.Assert(t, strings.Contains(out, "using package manager pip"),
				"expected install to run regardless of caching; output: %s", out)
			assert.Equal(t, len(stubCalls(t, logPath)) >= 1, true, "expected the install tool to be invoked")

			saved := len(s.Cache.SavedKeys()) > 0
			assert.Equal(t, saved, tc.wantSaved, "unexpected save state; keys: %v", s.Cache.SavedKeys())
		})
	}
}

// TestCLI_InstallPackages_InstallArgs checks the binary and argv the CLI hands
// each package manager, observed through the recording stub. --cache=false
// keeps the log to just the install invocation (no python-version probe for
// the cache key).
func TestCLI_InstallPackages_InstallArgs(t *testing.T) {
	tests := []struct {
		name       string
		pkgManager string
		tool       string
		fixture    string
		extraArgs  []string
		wantCall   string
	}{
		// --args must be one "--args=value" token: kong otherwise misparses a
		// value starting with "--" as the next flag.
		{name: "pip", pkgManager: "pip", fixture: "pip", extraArgs: []string{"--args=--no-deps --quiet"}, wantCall: "pip install -r requirements.txt --no-deps --quiet"},
		{name: "pip-dist", pkgManager: "pip-dist", fixture: "pip-dist", wantCall: "pip install -e ."},
		{name: "poetry", pkgManager: "poetry", tool: "poetry", fixture: "poetry", wantCall: "poetry install --no-ansi"},
		{name: "pipenv", pkgManager: "pipenv", tool: "pipenv", fixture: "pipenv", wantCall: "pipenv install"},
		{name: "uv", pkgManager: "uv", tool: "uv", fixture: "uv", wantCall: "uv sync"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, run := sandbox(t)
			env, logPath := stubTools(t, append(pythonStubs, tc.tool)...)
			appDir := copyFixture(t, tc.fixture)

			args := append([]string{"install-packages", "--app-dir", appDir, "--package-manager", tc.pkgManager, "--cache=false"}, tc.extraArgs...)
			out, err := run(t, env, args...)
			assert.NilError(t, err, "output: %s", out)

			calls := stubCalls(t, logPath)
			assert.Equal(t, len(calls), 1, "expected exactly the install invocation; got: %v", calls)
			assert.Equal(t, calls[0], tc.wantCall)
		})
	}
}

// TestCLI_InstallPackages_AliasLinkFailureWarns covers an executor with only
// python3 and pip3, in a directory install-packages can't link python and pip
// into: it warns and installs with pip3 anyway.
func TestCLI_InstallPackages_AliasLinkFailureWarns(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "the stubs are shell scripts")
	skip.If(t, os.Getuid() == 0, "root can write to a read-only directory")

	_, run := sandbox(t)
	env, logPath := stubTools(t, "python3", "pip3")
	pathDir := env["PATH"]
	//nolint:gosec // test stub; 0700 so it is executable
	assert.NilError(t, os.WriteFile(filepath.Join(pathDir, "python3"), []byte("#!/bin/sh\necho 'Python 3.12.1'\n"), 0700))
	//nolint:gosec // a directory needs its execute bit to be searched on PATH
	assert.NilError(t, os.Chmod(pathDir, 0500))
	//nolint:gosec // writable again, so the temp dir can be removed
	t.Cleanup(func() { _ = os.Chmod(pathDir, 0700) })

	appDir := copyFixture(t, "pip")
	out, err := run(t, env, "install-packages", "--app-dir", appDir, "--cache=false")
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "warning: link python to python3"), "output: %s", out)
	assert.DeepEqual(t, stubCalls(t, logPath), []string{"pip3 install -r requirements.txt"})
}
