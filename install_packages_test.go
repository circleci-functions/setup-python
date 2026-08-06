package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/backplane-go/testing/testcontext"

	"github.com/circleci-functions/setup-python/python"
	"github.com/circleci-functions/toolkit/sdk"
	"github.com/circleci-functions/toolkit/sdk/sdktest"
)

// stubRunInstall replaces the runInstall seam for the duration of the test
// and returns a pointer to the call count.
func stubRunInstall(t *testing.T, fn func(ctx context.Context, bin string, args []string, appDir string) error) *int {
	t.Helper()
	calls := 0
	orig := runInstall
	runInstall = func(ctx context.Context, bin string, args []string, appDir string) error {
		calls++
		return fn(ctx, bin, args, appDir)
	}
	t.Cleanup(func() { runInstall = orig })
	return &calls
}

// countingCacheServer starts a plugin server that counts every cache request
// and reports success. It returns a client for that server and a func
// reporting how many requests it has received.
func countingCacheServer(t *testing.T) (*sdk.Client, func() int) {
	t.Helper()
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "success"})
	}))
	t.Cleanup(srv.Close)
	return sdk.NewWithBaseURL(srv.URL), func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
}

// setHermeticEnv isolates the test from the host: a scratch HOME, no leaked
// pipenv or branch env vars, fake python and pip on PATH, and a scratch
// BASH_ENV.
func setHermeticEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{"WORKON_HOME", "PIPENV_VENV_IN_PROJECT", "CIRCLE_WORKING_DIRECTORY", "CIRCLE_BRANCH"} {
		t.Setenv(key, "")
		assert.NilError(t, os.Unsetenv(key))
	}

	binDir := t.TempDir()
	for _, name := range []string{"python", "pip"} {
		assert.NilError(t, os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\n"), 0755)) //nolint:gosec
	}
	t.Setenv("PATH", binDir)
	t.Setenv("BASH_ENV", filepath.Join(t.TempDir(), "env.sh"))
}

// cachePaths resolves the venv and pypi paths Run will restore and save.
func cachePaths(t *testing.T, pm python.PackageManager, appDir string) []string {
	t.Helper()
	venvPaths, err := python.VenvPaths(pm, appDir, nil)
	assert.NilError(t, err)
	pypiPaths, err := python.PypiPaths(pm)
	assert.NilError(t, err)
	paths := append([]string{}, venvPaths...)
	paths = append(paths, pypiPaths...)
	assert.Assert(t, len(paths) > 0, "expected at least one cache path for %s", pm)
	return paths
}

// TestInstallPackagesCmd_CacheRoundTrip proves the restore, install, save
// cycle: a marker file saved by a first run is restored by a second run
// before install, once the cache path has been wiped in between.
func TestInstallPackagesCmd_CacheRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		pkgManager string
		setupFiles func(t *testing.T, appDir string)
	}{
		{
			name:       "pip has no venv and caches pypi only",
			pkgManager: "pip",
			setupFiles: func(t *testing.T, appDir string) {
				assert.NilError(t, os.WriteFile(filepath.Join(appDir, "requirements.txt"), []byte("requests==2.31.0\n"), 0600))
			},
		},
		{
			name:       "poetry caches its venv",
			pkgManager: "poetry",
			setupFiles: func(t *testing.T, appDir string) {
				assert.NilError(t, os.WriteFile(filepath.Join(appDir, "pyproject.toml"), []byte("[tool.poetry]\nname = \"x\"\n"), 0600))
				assert.NilError(t, os.WriteFile(filepath.Join(appDir, "poetry.lock"), []byte("lock-contents\n"), 0600))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := sdktest.NewServer(t)
			client := srv.Client()
			ctx := testcontext.New(t)

			setHermeticEnv(t)
			appDir := t.TempDir()
			tc.setupFiles(t, appDir)

			installs := stubRunInstall(t, func(context.Context, string, []string, string) error { return nil })

			cmd := &installPackagesCmd{
				AppDir:            appDir,
				CacheVersion:      "v1",
				PathArgs:          ".",
				PipDependencyFile: "requirements.txt",
				PkgManager:        tc.pkgManager,
				Cache:             true,
				PypiCache:         true,
				VenvCache:         true,
			}

			pm, err := cmd.resolvePackageManager()
			assert.NilError(t, err)
			paths := cachePaths(t, pm, appDir)
			cachedDir := paths[0]

			// SaveCache walks every path, so all must exist even though only
			// cachedDir gets a marker.
			for _, p := range paths {
				assert.NilError(t, os.MkdirAll(p, 0750))
			}
			assert.NilError(t, os.WriteFile(filepath.Join(cachedDir, "marker"), []byte("hit"), 0600))

			assert.NilError(t, cmd.Run(ctx, client))
			assert.Equal(t, *installs, 1, "expected install to run once")

			assert.NilError(t, os.RemoveAll(cachedDir))

			assert.NilError(t, cmd.Run(ctx, client))
			assert.Equal(t, *installs, 2, "expected install to run again")

			got, err := os.ReadFile(filepath.Join(cachedDir, "marker")) //nolint:gosec
			assert.NilError(t, err)
			assert.Equal(t, string(got), "hit", "expected marker restored from the first run's save")
		})
	}
}

// TestInstallPackagesCmd_CacheFlags checks which cache requests each
// combination of --cache, --pypi-cache and --venv-cache issues. Install
// always runs.
func TestInstallPackagesCmd_CacheFlags(t *testing.T) {
	tests := []struct {
		name      string
		cache     bool
		pypiCache bool
		venvCache bool
		wantCalls int
	}{
		{name: "cache enabled restores and saves", cache: true, pypiCache: true, venvCache: true, wantCalls: 2},
		{name: "no-cache skips both", cache: false, pypiCache: true, venvCache: true, wantCalls: 0},
		{name: "no cache buckets selected skips both", cache: true, pypiCache: false, venvCache: false, wantCalls: 0},
		{name: "pypi only still round trips", cache: true, pypiCache: true, venvCache: false, wantCalls: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, calls := countingCacheServer(t)
			ctx := testcontext.New(t)

			setHermeticEnv(t)
			appDir := t.TempDir()
			assert.NilError(t, os.WriteFile(filepath.Join(appDir, "requirements.txt"), []byte("requests==2.31.0\n"), 0600))

			installs := stubRunInstall(t, func(context.Context, string, []string, string) error { return nil })

			cmd := &installPackagesCmd{
				AppDir:            appDir,
				CacheVersion:      "v1",
				PathArgs:          ".",
				PipDependencyFile: "requirements.txt",
				PkgManager:        "pip",
				Cache:             tc.cache,
				PypiCache:         tc.pypiCache,
				VenvCache:         tc.venvCache,
			}

			assert.NilError(t, cmd.Run(ctx, client))
			assert.Equal(t, *installs, 1, "expected install to run regardless of caching")
			assert.Equal(t, calls(), tc.wantCalls)
		})
	}
}

// TestInstallPackagesCmd_InstallArgs checks the binary and argv Run passes to
// runInstall for each package manager.
func TestInstallPackagesCmd_InstallArgs(t *testing.T) {
	tests := []struct {
		name       string
		pkgManager string
		args       string
		wantBins   []string
		wantArgs   []string
	}{
		{
			name:       "pip installs from the dependency file",
			pkgManager: "pip",
			args:       "--no-deps --quiet",
			wantBins:   []string{"pip", "pip3"},
			wantArgs:   []string{"install", "-r", "requirements.txt", "--no-deps", "--quiet"},
		},
		{
			name:       "pip-dist installs the project as editable",
			pkgManager: "pip-dist",
			wantBins:   []string{"pip", "pip3"},
			wantArgs:   []string{"install", "-e", "."},
		},
		{
			name:       "poetry installs without ansi",
			pkgManager: "poetry",
			wantBins:   []string{"poetry"},
			wantArgs:   []string{"install", "--no-ansi"},
		},
		{
			name:       "pipenv installs",
			pkgManager: "pipenv",
			wantBins:   []string{"pipenv"},
			wantArgs:   []string{"install"},
		},
		{
			name:       "uv syncs",
			pkgManager: "uv",
			wantBins:   []string{"uv"},
			wantArgs:   []string{"sync"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setHermeticEnv(t)
			appDir := t.TempDir()
			assert.NilError(t, os.WriteFile(filepath.Join(appDir, "requirements.txt"), []byte("x\n"), 0600))

			client, _ := countingCacheServer(t)
			ctx := testcontext.New(t)

			var gotBin, gotDir string
			var gotArgs []string
			stubRunInstall(t, func(_ context.Context, bin string, args []string, dir string) error {
				gotBin, gotArgs, gotDir = bin, args, dir
				return nil
			})

			cmd := &installPackagesCmd{
				AppDir:            appDir,
				CacheVersion:      "v1",
				PathArgs:          ".",
				PipDependencyFile: "requirements.txt",
				PkgManager:        tc.pkgManager,
				Args:              tc.args,
				Cache:             false,
			}

			assert.NilError(t, cmd.Run(ctx, client))
			assert.DeepEqual(t, gotArgs, tc.wantArgs)
			assert.Equal(t, gotDir, appDir)
			assert.Assert(t, contains(tc.wantBins, gotBin), "got binary %q, want one of %v", gotBin, tc.wantBins)
		})
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
