package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/backplane-go/testing/testcontext"

	"github.com/circleci/functions/cmd/setup-python/python"
	"github.com/circleci/functions/pkg/sdk/sdktest"
)

// stubRunInstall replaces the runInstall seam for the duration of the test,
// restoring the original on cleanup, so tests can prove the
// restore->install->save orchestration in installPackagesCmd.Run without
// exec-ing a real pip/poetry/pipenv/uv.
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

// setHermeticEnv points HOME at a fresh temp dir, clears the pipenv/branch
// env vars that would otherwise leak from the host running the test (so
// cache paths and cache keys are deterministic), puts fake python/pip
// executables on PATH (so ResolveBinaries doesn't depend on the host's own
// PATH and never needs to write a python3/pip3 alias), and points BASH_ENV
// at a scratch file so EnsureAliased never errors even if it did.
func setHermeticEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WORKON_HOME", "")
	assert.NilError(t, os.Unsetenv("WORKON_HOME"))
	t.Setenv("PIPENV_VENV_IN_PROJECT", "")
	assert.NilError(t, os.Unsetenv("PIPENV_VENV_IN_PROJECT"))
	t.Setenv("CIRCLE_WORKING_DIRECTORY", "")
	assert.NilError(t, os.Unsetenv("CIRCLE_WORKING_DIRECTORY"))

	binDir := t.TempDir()
	for _, name := range []string{"python", "pip"} {
		assert.NilError(t, os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\n"), 0755)) //nolint:gosec
	}
	t.Setenv("PATH", binDir)
	t.Setenv("BASH_ENV", filepath.Join(t.TempDir(), "env.sh"))
}

// cachePaths replicates installPackagesCmd.Run's venv+pypi path resolution,
// so tests can locate the exact directories the command will restore/save.
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

// TestInstallPackagesCmd_CacheRoundTrip proves the full
// restore -> install -> save cycle for pkg-manager=pip (no venv, pypi-only)
// and pkg-manager=poetry (venv-based): a marker file present in the cache
// path at the time of a first run is captured by SaveCache; once the path is
// wiped, a second run's RestoreCache (which happens before install) brings
// the marker back before install runs again.
func TestInstallPackagesCmd_CacheRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		pkgManager string
		setupFiles func(t *testing.T, appDir string)
	}{
		{
			name:       "pip",
			pkgManager: "pip",
			setupFiles: func(t *testing.T, appDir string) {
				assert.NilError(t, os.WriteFile(filepath.Join(appDir, "requirements.txt"), []byte("requests==2.31.0\n"), 0600))
			},
		},
		{
			name:       "poetry",
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

			// SaveCache walks every path's live filesystem contents, so all
			// of them must exist even though only cachedDir gets a marker.
			for _, p := range paths {
				assert.NilError(t, os.MkdirAll(p, 0750))
			}
			assert.NilError(t, os.WriteFile(filepath.Join(cachedDir, "marker"), []byte("hit"), 0600))

			// First run: cache miss on restore (nothing saved yet), install
			// runs, then save captures the marker file already on disk.
			assert.NilError(t, cmd.Run(ctx, client))
			assert.Equal(t, *installs, 1, "expected install to run once")
			assert.Equal(t, srv.RestoreCacheCalls(), 1, "expected exactly one RestoreCache call")
			assert.Equal(t, srv.SaveCacheCalls(), 1, "expected exactly one SaveCache call")

			assert.NilError(t, os.RemoveAll(cachedDir))

			// Second run, same cache key: restore (before install) must
			// repopulate the marker file from the first run's save.
			assert.NilError(t, cmd.Run(ctx, client))
			assert.Equal(t, *installs, 2, "expected install to run again")
			assert.Equal(t, srv.RestoreCacheCalls(), 2)
			assert.Equal(t, srv.SaveCacheCalls(), 2)

			got, err := os.ReadFile(filepath.Join(cachedDir, "marker")) //nolint:gosec
			assert.NilError(t, err)
			assert.Equal(t, string(got), "hit", "expected marker file restored from the first run's save")
		})
	}
}

// TestInstallPackagesCmd_NoCache_SkipsCacheCalls verifies that --no-cache
// (Cache: false) results in zero RestoreCache/SaveCache calls to the fake
// server, even though install still runs.
func TestInstallPackagesCmd_NoCache_SkipsCacheCalls(t *testing.T) {
	srv := sdktest.NewServer(t)
	client := srv.Client()
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
		Cache:             false,
		PypiCache:         true,
		VenvCache:         true,
	}

	assert.NilError(t, cmd.Run(ctx, client))
	assert.Equal(t, *installs, 1, "expected install to still run")
	assert.Equal(t, srv.RestoreCacheCalls(), 0, "expected no RestoreCache calls with --no-cache")
	assert.Equal(t, srv.SaveCacheCalls(), 0, "expected no SaveCache calls with --no-cache")
}

// TestInstallPackagesCmd_InstallArgs verifies that Run invokes runInstall
// with the exact binary and argv the detected/selected package manager's
// InstallArgs builds, mirroring porting.md's install-packages command table.
func TestInstallPackagesCmd_InstallArgs(t *testing.T) {
	setHermeticEnv(t)
	appDir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(appDir, "requirements.txt"), []byte("x\n"), 0600))

	srv := sdktest.NewServer(t)
	client := srv.Client()
	ctx := testcontext.New(t)

	var gotBin string
	var gotArgs []string
	var gotDir string
	stubRunInstall(t, func(_ context.Context, bin string, args []string, dir string) error {
		gotBin, gotArgs, gotDir = bin, args, dir
		return nil
	})

	cmd := &installPackagesCmd{
		AppDir:            appDir,
		CacheVersion:      "v1",
		PathArgs:          ".",
		PipDependencyFile: "requirements.txt",
		PkgManager:        "pip",
		Args:              "--no-deps --quiet",
		Cache:             false,
	}

	assert.NilError(t, cmd.Run(ctx, client))
	assert.DeepEqual(t, gotArgs, []string{"install", "-r", "requirements.txt", "--no-deps", "--quiet"})
	assert.Assert(t, gotBin == "pip" || gotBin == "pip3", "expected pip or pip3, got %q", gotBin)
	assert.Equal(t, gotDir, appDir)
}
