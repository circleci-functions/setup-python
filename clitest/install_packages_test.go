package clitest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
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
// no venv), and --no-pypi-cache drops the fixed per-tool pypi paths so the
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
				"--pkg-manager", tc.pkgManager,
				"--venv-path", scratch,
				"--no-pypi-cache",
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
		{name: "cache enabled saves", args: []string{"--venv-path", "SCRATCH", "--no-pypi-cache"}, wantSaved: true},
		{name: "no-cache skips the save", args: []string{"--no-cache"}, wantSaved: false},
		{name: "no cache buckets selected skips the save", args: []string{"--no-pypi-cache", "--no-venv-cache"}, wantSaved: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, run := sandbox(t)
			env, logPath := stubTools(t, pythonStubs...)
			appDir := copyFixture(t, "pip")

			args := []string{"install-packages", "--app-dir", appDir, "--pkg-manager", "pip"}
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
// each package manager, observed through the recording stub. --no-cache keeps
// the log to just the install invocation (no python-version probe for the
// cache key).
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

			args := append([]string{"install-packages", "--app-dir", appDir, "--pkg-manager", tc.pkgManager, "--no-cache"}, tc.extraArgs...)
			out, err := run(t, env, args...)
			assert.NilError(t, err, "output: %s", out)

			calls := stubCalls(t, logPath)
			assert.Equal(t, len(calls), 1, "expected exactly the install invocation; got: %v", calls)
			assert.Equal(t, calls[0], tc.wantCall)
		})
	}
}
