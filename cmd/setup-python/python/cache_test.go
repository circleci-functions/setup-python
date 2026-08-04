// Internal test package (not python_test) so tests can stub the
// pythonVersion seam without depending on a real python interpreter being
// on PATH.
package python

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/backplane-go/testing/testcontext"

	"github.com/circleci/functions/pkg/sdk"
	"github.com/circleci/functions/pkg/sdk/sdktest"
)

// stubPythonVersion replaces the pythonVersion seam for the duration of the
// test, restoring the original on cleanup.
func stubPythonVersion(t *testing.T, version string, err error) {
	t.Helper()
	orig := pythonVersion
	pythonVersion = func() (string, error) { return version, err }
	t.Cleanup(func() { pythonVersion = orig })
}

// --- CacheKey ---

func TestCacheKeyToggleCombinations(t *testing.T) {
	stubPythonVersion(t, "Python 3.11.4", nil)

	dir := t.TempDir()
	lockfile := filepath.Join(dir, "requirements.txt")
	contents := []byte("requests==2.31.0\n")
	assert.NilError(t, os.WriteFile(lockfile, contents, 0600))
	sum := fmt.Sprintf("%x", sha256.Sum256(contents))

	tests := []struct {
		name          string
		includeBranch bool
		includePython bool
		branch        string
		want          string
	}{
		{
			name: "neither branch nor python",
			want: "v1-cci_pycache-" + sum,
		},
		{
			name:          "branch only",
			includeBranch: true,
			branch:        "main",
			want:          "v1-cci_pycache-main-" + sum,
		},
		{
			name:          "python only",
			includePython: true,
			want:          "v1-cci_pycache-Python 3.11.4-" + sum,
		},
		{
			name:          "branch and python",
			includeBranch: true,
			includePython: true,
			branch:        "feature/x",
			want:          "v1-cci_pycache-feature/x-Python 3.11.4-" + sum,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CIRCLE_BRANCH", tc.branch)

			got, err := CacheKey("v1", tc.includeBranch, tc.includePython, lockfile, "")
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

// TestCacheKeyBranchEnvUnset verifies that an unset CIRCLE_BRANCH still
// contributes a bare "-" separator, matching the literal
// "-" + os.Getenv("CIRCLE_BRANCH") construction.
func TestCacheKeyBranchEnvUnset(t *testing.T) {
	t.Setenv("CIRCLE_BRANCH", "")
	_ = os.Unsetenv("CIRCLE_BRANCH")

	dir := t.TempDir()
	lockfile := filepath.Join(dir, "requirements.txt")
	contents := []byte("flask==3.0.0\n")
	assert.NilError(t, os.WriteFile(lockfile, contents, 0600))
	sum := fmt.Sprintf("%x", sha256.Sum256(contents))

	got, err := CacheKey("v1", true, false, lockfile, "")
	assert.NilError(t, err)
	// includeBranch still contributes its "-" separator even though
	// CIRCLE_BRANCH itself is empty, per the literal
	// "-" + os.Getenv("CIRCLE_BRANCH") construction -- hence the double
	// dash before the checksum.
	assert.Equal(t, got, "v1-cci_pycache--"+sum)
}

// TestCacheKeyOverrideSkipsComputation verifies that a non-empty override is
// returned verbatim, without even attempting to read the (nonexistent)
// lockfile or resolve a python version.
func TestCacheKeyOverrideSkipsComputation(t *testing.T) {
	stubPythonVersion(t, "", fmt.Errorf("should not be called"))

	got, err := CacheKey("v1", true, true, filepath.Join(t.TempDir(), "does-not-exist.lock"), "my-override-key")
	assert.NilError(t, err)
	assert.Equal(t, got, "my-override-key")
}

// TestCacheKeyMissingLockfile verifies that a missing lockfile is a hard
// error when no override is set.
func TestCacheKeyMissingLockfile(t *testing.T) {
	_, err := CacheKey("v1", false, false, filepath.Join(t.TempDir(), "does-not-exist.lock"), "")
	assert.ErrorContains(t, err, "read lockfile")
}

// TestCacheKeyPythonVersionError verifies that a failure resolving the
// python version (e.g. no interpreter on PATH) is a hard error, not
// silently swallowed.
func TestCacheKeyPythonVersionError(t *testing.T) {
	stubPythonVersion(t, "", fmt.Errorf("python: command not found"))

	dir := t.TempDir()
	lockfile := filepath.Join(dir, "requirements.txt")
	assert.NilError(t, os.WriteFile(lockfile, []byte("x"), 0600))

	_, err := CacheKey("v1", false, true, lockfile, "")
	assert.ErrorContains(t, err, "resolve python version")
}

// --- VenvPaths ---

func TestCacheVenvPathsPipHasNone(t *testing.T) {
	for _, pm := range []PackageManager{Pip, PipDist} {
		t.Run(string(pm), func(t *testing.T) {
			got, err := VenvPaths(pm, "/app", nil)
			assert.NilError(t, err)
			assert.Assert(t, len(got) == 0, "expected no venv paths for %s, got %v", pm, got)
		})
	}
}

func TestCacheVenvPathsPipenv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	appDir := "/app"

	t.Run("defaults to ~/.local/share/virtualenvs", func(t *testing.T) {
		t.Setenv("WORKON_HOME", "")
		_ = os.Unsetenv("WORKON_HOME")
		t.Setenv("PIPENV_VENV_IN_PROJECT", "")
		_ = os.Unsetenv("PIPENV_VENV_IN_PROJECT")

		got, err := VenvPaths(Pipenv, appDir, nil)
		assert.NilError(t, err)
		assert.DeepEqual(t, got, []string{filepath.Join(home, ".local", "share", "virtualenvs")})
	})

	t.Run("WORKON_HOME set", func(t *testing.T) {
		_ = os.Unsetenv("PIPENV_VENV_IN_PROJECT")
		t.Setenv("WORKON_HOME", "/custom/workon-home")

		got, err := VenvPaths(Pipenv, appDir, nil)
		assert.NilError(t, err)
		assert.DeepEqual(t, got, []string{"/custom/workon-home"})
	})

	t.Run("PIPENV_VENV_IN_PROJECT overrides WORKON_HOME", func(t *testing.T) {
		t.Setenv("WORKON_HOME", "/custom/workon-home")
		t.Setenv("PIPENV_VENV_IN_PROJECT", "1")

		got, err := VenvPaths(Pipenv, appDir, nil)
		assert.NilError(t, err)
		assert.DeepEqual(t, got, []string{filepath.Join(appDir, ".venvs")})
	})
}

func TestCacheVenvPathsPoetry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := VenvPaths(Poetry, "/app", nil)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []string{filepath.Join(home, ".cache", "pypoetry", "virtualenvs")})
}

func TestCacheVenvPathsUV(t *testing.T) {
	appDir := "/app"

	t.Run("defaults to <app-dir>/.venv", func(t *testing.T) {
		_ = os.Unsetenv("CIRCLE_WORKING_DIRECTORY")

		got, err := VenvPaths(UV, appDir, nil)
		assert.NilError(t, err)
		assert.DeepEqual(t, got, []string{filepath.Join(appDir, ".venv")})
	})

	t.Run("CIRCLE_WORKING_DIRECTORY set", func(t *testing.T) {
		t.Setenv("CIRCLE_WORKING_DIRECTORY", "/circle/wd")

		got, err := VenvPaths(UV, appDir, nil)
		assert.NilError(t, err)
		assert.DeepEqual(t, got, []string{filepath.Join("/circle/wd", ".venv")})
	})
}

// TestCacheVenvPathsOverride verifies that a non-empty override replaces the
// detected path(s) for every manager, including pip/pip-dist which
// otherwise have no venv path at all.
func TestCacheVenvPathsOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	override := []string{"/override/one", "/override/two"}

	for _, pm := range []PackageManager{Pip, PipDist, Pipenv, Poetry, UV} {
		t.Run(string(pm), func(t *testing.T) {
			got, err := VenvPaths(pm, "/app", override)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, override)
		})
	}
}

// --- PypiPaths ---

func TestCachePypiPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		pm   PackageManager
		want []string
	}{
		{Pip, []string{
			filepath.Join(home, ".cache", "pip"),
			filepath.Join(home, ".pyenv", "versions"),
			filepath.Join(home, ".local", "lib"),
		}},
		{PipDist, []string{
			filepath.Join(home, ".cache", "pip"),
			filepath.Join(home, ".pyenv", "versions"),
			filepath.Join(home, ".local", "lib"),
		}},
		{Pipenv, []string{
			filepath.Join(home, ".cache", "pip"),
			filepath.Join(home, ".cache", "pipenv"),
		}},
		{Poetry, []string{filepath.Join(home, ".cache", "pip")}},
		{UV, []string{filepath.Join(home, ".cache", "uv")}},
	}

	for _, tc := range tests {
		t.Run(string(tc.pm), func(t *testing.T) {
			got, err := PypiPaths(tc.pm)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tc.want)
		})
	}
}

// --- RestoreCache / SaveCache ---

// TestCacheRestoreAndSaveRoundTrip verifies that saving under a key and
// restoring under the same key round-trips a file written into the cached
// directory.
func TestCacheRestoreAndSaveRoundTrip(t *testing.T) {
	srv := sdktest.NewServer(t)
	client := srv.Client()
	ctx := testcontext.New(t)

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
	ctx := testcontext.New(t)

	RestoreCache(ctx, client, "the-key")
	SaveCache(ctx, client, "the-key", []string{t.TempDir()})
}
