// Internal test package (not python_test) so tests can stub the
// pythonVersion seam without depending on a real python interpreter being
// on PATH.
package python

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk"
	"github.com/circleci-functions/toolkit/sdk/sdktest"
)

// stubPythonVersion replaces the pythonVersion seam for the duration of the
// test, restoring the original on cleanup.
func stubPythonVersion(t *testing.T, version string, err error) {
	t.Helper()
	orig := pythonVersion
	pythonVersion = func() (string, error) { return version, err }
	t.Cleanup(func() { pythonVersion = orig })
}

// unixOrWindows returns windows on windows and unix everywhere else, so
// expected paths follow the same runtime.GOOS branch as the code under test.
func unixOrWindows[T any](unix, windows T) T {
	if runtime.GOOS == "windows" {
		return windows
	}
	return unix
}

// userDirs points the OS home-directory lookup at a temporary directory and
// returns the home and user cache directories resolved from it. Expected
// paths are derived from these rather than hardcoded, because the user cache
// directory is platform specific (~/.cache, ~/Library/Caches,
// %LocalAppData%).
func userDirs(t *testing.T) (home, cache string) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}

	home, err := os.UserHomeDir()
	assert.NilError(t, err)
	cache, err = os.UserCacheDir()
	assert.NilError(t, err)

	return home, cache
}

// setEnv sets the variables present in env and clears every other name in
// names, so a case sees exactly the environment it declares and can't
// inherit one from a sibling case or from the developer's shell.
func setEnv(t *testing.T, names []string, env map[string]string) {
	t.Helper()
	for _, name := range names {
		// t.Setenv registers the restore even when the variable is
		// immediately unset again.
		t.Setenv(name, env[name])
		if _, ok := env[name]; !ok {
			_ = os.Unsetenv(name)
		}
	}
}

// venvEnvVars are the environment variables VenvPaths consults.
var venvEnvVars = []string{"PIPENV_VENV_IN_PROJECT", "WORKON_HOME", "CIRCLE_WORKING_DIRECTORY"}

// TestCacheVenvPaths verifies the virtualenv directories reported for each
// package manager: pip and pip-dist have none, pipenv follows
// PIPENV_VENV_IN_PROJECT then WORKON_HOME then its per-user default, poetry
// nests them in its own cache dir, uv follows CIRCLE_WORKING_DIRECTORY then
// the app dir, an explicit override replaces the lot, and an unrecognized
// manager is an error.
func TestCacheVenvPaths(t *testing.T) {
	home, cache := userDirs(t)
	appDir := filepath.Join(home, "app")
	override := []string{filepath.Join(home, "override", "one"), filepath.Join(home, "override", "two")}

	type testCase struct {
		name     string
		pm       PackageManager
		env      map[string]string
		override []string
		want     []string
		wantErr  string
	}

	tests := []testCase{
		{
			name: "pip installs globally so has no venv",
			pm:   Pip,
		},
		{
			name: "pip-dist installs globally so has no venv",
			pm:   PipDist,
		},
		{
			name: "pipenv defaults to the per-user virtualenvs dir",
			pm:   Pipenv,
			want: unixOrWindows(
				[]string{filepath.Join(home, ".local", "share", "virtualenvs")},
				[]string{filepath.Join(home, ".virtualenvs")},
			),
		},
		{
			name: "pipenv honours WORKON_HOME",
			pm:   Pipenv,
			env:  map[string]string{"WORKON_HOME": filepath.Join(home, "workon")},
			want: []string{filepath.Join(home, "workon")},
		},
		{
			name: "pipenv prefers PIPENV_VENV_IN_PROJECT over WORKON_HOME",
			pm:   Pipenv,
			env: map[string]string{
				"WORKON_HOME":            filepath.Join(home, "workon"),
				"PIPENV_VENV_IN_PROJECT": "1",
			},
			want: []string{filepath.Join(appDir, ".venvs")},
		},
		{
			name: "poetry keeps virtualenvs under its own cache dir",
			pm:   Poetry,
			want: unixOrWindows(
				[]string{filepath.Join(cache, "pypoetry", "virtualenvs")},
				[]string{filepath.Join(cache, "pypoetry", "Cache", "virtualenvs")},
			),
		},
		{
			name: "uv defaults to <app-dir>/.venv",
			pm:   UV,
			want: []string{filepath.Join(appDir, ".venv")},
		},
		{
			name: "uv prefers CIRCLE_WORKING_DIRECTORY over the app dir",
			pm:   UV,
			env:  map[string]string{"CIRCLE_WORKING_DIRECTORY": filepath.Join(home, "circle-wd")},
			want: []string{filepath.Join(home, "circle-wd", ".venv")},
		},
		{
			name:    "unknown package manager is an error",
			pm:      "bogus",
			wantErr: `unknown package manager "bogus"`,
		},
	}

	// An override replaces the detected paths for every manager, including
	// pip and pip-dist, which otherwise have none at all.
	for _, pm := range []PackageManager{Pip, PipDist, Pipenv, Poetry, UV} {
		tests = append(tests, testCase{
			name:     "override wins for " + string(pm),
			pm:       pm,
			override: override,
			want:     override,
		})
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, venvEnvVars, tc.env)

			got, err := VenvPaths(tc.pm, appDir, tc.override)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tc.want)
		})
	}
}

// TestCachePypiPaths verifies the download and site-packages directories
// reported for each package manager, and that an unrecognized manager is an
// error.
func TestCachePypiPaths(t *testing.T) {
	home, cache := userDirs(t)

	pipCache := unixOrWindows(
		filepath.Join(cache, "pip"),
		filepath.Join(cache, "pip", "Cache"),
	)
	pipenvCache := unixOrWindows(
		filepath.Join(cache, "pipenv"),
		filepath.Join(cache, "pipenv", "Cache"),
	)
	uvCache := unixOrWindows(
		filepath.Join(cache, "uv"),
		filepath.Join(cache, "uv", "cache"),
	)
	pyenv := unixOrWindows(
		filepath.Join(home, ".pyenv", "versions"),
		filepath.Join(home, ".pyenv", "pyenv-win", "versions"),
	)
	userLib := unixOrWindows(
		filepath.Join(home, ".local", "lib"),
		filepath.Join(os.Getenv("APPDATA"), "Python"),
	)

	tests := []struct {
		name    string
		pm      PackageManager
		want    []string
		wantErr string
	}{
		{
			name: "pip caches downloads plus the pyenv and user lib trees",
			pm:   Pip,
			want: []string{pipCache, pyenv, userLib},
		},
		{
			name: "pip-dist matches pip",
			pm:   PipDist,
			want: []string{pipCache, pyenv, userLib},
		},
		{
			name: "pipenv adds its own cache to pip's",
			pm:   Pipenv,
			want: []string{pipCache, pipenvCache},
		},
		{
			name: "poetry downloads through pip",
			pm:   Poetry,
			want: []string{pipCache},
		},
		{
			name: "uv uses only its own cache",
			pm:   UV,
			want: []string{uvCache},
		},
		{
			name:    "unknown package manager is an error",
			pm:      "bogus",
			wantErr: `unknown package manager "bogus"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PypiPaths(tc.pm)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tc.want)
		})
	}
}

// TestCacheKey verifies the composition of the cache key: the branch and
// python version components appear only when asked for, the lockfile
// checksum always terminates the key, a non-empty override short-circuits
// everything, and a missing lockfile or unresolvable python version is an
// error, the missing lockfile reported as ErrNoLockfile.
func TestCacheKey(t *testing.T) {
	dir := t.TempDir()
	lockfile := filepath.Join(dir, "requirements.txt")
	contents := []byte("requests==2.31.0\n")
	assert.NilError(t, os.WriteFile(lockfile, contents, 0600))
	sum := fmt.Sprintf("%x", sha256.Sum256(contents))

	missing := filepath.Join(dir, "does-not-exist.lock")

	// Stubbed in for the cases that must not consult a python interpreter:
	// if they do, the key computation fails and the case fails with it.
	errNotCalled := errors.New("pythonVersion should not have been called")

	tests := []struct {
		name          string
		includeBranch bool
		includePython bool
		env           map[string]string
		lockfile      string
		override      string
		version       string
		versionErr    error
		want          string
		wantErr       string
		wantErrIs     error
	}{
		{
			name:       "neither branch nor python version",
			lockfile:   lockfile,
			versionErr: errNotCalled,
			want:       "v1-cci_pycache-" + sum,
		},
		{
			name:          "branch only",
			includeBranch: true,
			env:           map[string]string{"CIRCLE_BRANCH": "main"},
			lockfile:      lockfile,
			versionErr:    errNotCalled,
			want:          "v1-cci_pycache-main-" + sum,
		},
		{
			name:          "python version only",
			includePython: true,
			lockfile:      lockfile,
			version:       "Python 3.11.4",
			want:          "v1-cci_pycache-Python 3.11.4-" + sum,
		},
		{
			name:          "branch and python version",
			includeBranch: true,
			includePython: true,
			env:           map[string]string{"CIRCLE_BRANCH": "feature/x"},
			lockfile:      lockfile,
			version:       "Python 3.11.4",
			want:          "v1-cci_pycache-feature/x-Python 3.11.4-" + sum,
		},
		{
			// The branch component is unconditionally prefixed with its
			// separator, so an unset CIRCLE_BRANCH leaves a bare "-" behind
			// -- hence the double dash before the checksum.
			name:          "branch requested but CIRCLE_BRANCH unset",
			includeBranch: true,
			lockfile:      lockfile,
			versionErr:    errNotCalled,
			want:          "v1-cci_pycache--" + sum,
		},
		{
			name:          "override is returned verbatim without reading anything",
			includeBranch: true,
			includePython: true,
			env:           map[string]string{"CIRCLE_BRANCH": "main"},
			lockfile:      missing,
			override:      "my-override-key",
			versionErr:    errNotCalled,
			want:          "my-override-key",
		},
		{
			name:          "missing lockfile is ErrNoLockfile, without consulting python",
			includePython: true,
			lockfile:      missing,
			versionErr:    errNotCalled,
			wantErr:       ErrNoLockfile.Error(),
			wantErrIs:     ErrNoLockfile,
		},
		{
			name:          "unresolvable python version is an error",
			includePython: true,
			lockfile:      lockfile,
			versionErr:    errors.New("python: command not found"),
			wantErr:       "resolve python version",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, []string{"CIRCLE_BRANCH"}, tc.env)
			stubPythonVersion(t, tc.version, tc.versionErr)

			got, err := CacheKey("v1", tc.includeBranch, tc.includePython, tc.lockfile, tc.override)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				if tc.wantErrIs != nil {
					assert.ErrorIs(t, err, tc.wantErrIs)
				}
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

// TestCacheRestoreAndSaveRoundTrip verifies that saving under a key and
// restoring under the same key round-trips a file written into the cached
// directory.
func TestCacheRestoreAndSaveRoundTrip(t *testing.T) {
	srv := sdktest.NewServer(t)
	client := srv.Client()
	ctx := context.Background()

	dir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "marker"), []byte("hit"), 0600))

	SaveCache(ctx, client, "the-key", []string{dir})
	assert.NilError(t, os.RemoveAll(dir))

	RestoreCache(ctx, client, "the-key")

	got, err := os.ReadFile(filepath.Join(dir, "marker")) //nolint:gosec
	assert.NilError(t, err)
	assert.Equal(t, string(got), "hit")
}

// TestCacheErrorIsNonFatal verifies that a client which can't reach a
// server never blocks restore/save.
func TestCacheErrorIsNonFatal(t *testing.T) {
	client := sdk.NewWithBaseURL("http://127.0.0.1:0")
	ctx := context.Background()

	RestoreCache(ctx, client, "the-key")
	SaveCache(ctx, client, "the-key", []string{t.TempDir()})
}
