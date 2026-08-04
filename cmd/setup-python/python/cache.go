package python

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/circleci/functions/pkg/sdk"
)

// cacheKeyTag identifies setup-python's cache keys, mirroring the
// circleci/python orb's own "cci_pycache" component.
const cacheKeyTag = "cci_pycache"

// VenvPaths returns the filesystem path(s) holding pm's virtual
// environment(s), per porting.md's cache path table. appDir is the project
// directory (the --app-dir flag), consulted for pipenv's
// PIPENV_VENV_IN_PROJECT case and uv's <app-dir>/.venv fallback.
//
// override, when non-empty, is the caller-supplied --venv-path flag value:
// it replaces the detected path(s) outright, for any manager (including
// pip/pip-dist, which otherwise have no venv path at all).
//
// pip and pip-dist have no venv concept and return (nil, nil) absent an
// override.
func VenvPaths(pm PackageManager, appDir string, override []string) ([]string, error) {
	if len(override) > 0 {
		return override, nil
	}

	switch pm {
	case Pip, PipDist:
		return nil, nil
	case Pipenv:
		if os.Getenv("PIPENV_VENV_IN_PROJECT") != "" {
			return []string{filepath.Join(appDir, ".venvs")}, nil
		}
		if workonHome := os.Getenv("WORKON_HOME"); workonHome != "" {
			return []string{workonHome}, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		return []string{filepath.Join(home, ".local", "share", "virtualenvs")}, nil
	case Poetry:
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		return []string{filepath.Join(home, ".cache", "pypoetry", "virtualenvs")}, nil
	case UV:
		if wd := os.Getenv("CIRCLE_WORKING_DIRECTORY"); wd != "" {
			return []string{filepath.Join(wd, ".venv")}, nil
		}
		return []string{filepath.Join(appDir, ".venv")}, nil
	default:
		return nil, fmt.Errorf("unknown package manager %q", pm)
	}
}

// PypiPaths returns the filesystem path(s) holding pm's package/download
// cache, per porting.md's cache path table. Unlike VenvPaths, there is no
// override for this bucket.
func PypiPaths(pm PackageManager) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	switch pm {
	case Pip, PipDist:
		return []string{
			filepath.Join(home, ".cache", "pip"),
			filepath.Join(home, ".pyenv", "versions"),
			filepath.Join(home, ".local", "lib"),
		}, nil
	case Pipenv:
		return []string{
			filepath.Join(home, ".cache", "pip"),
			filepath.Join(home, ".cache", "pipenv"),
		}, nil
	case Poetry:
		return []string{filepath.Join(home, ".cache", "pip")}, nil
	case UV:
		return []string{filepath.Join(home, ".cache", "uv")}, nil
	default:
		return nil, fmt.Errorf("unknown package manager %q", pm)
	}
}

// CacheKey builds the cache key for install-packages's venv/pypi cache
// buckets. If override (the --cache-key-override flag) is non-empty, it's
// returned verbatim and nothing else is computed -- notably, the lockfile
// at lockfilePath is not even read. Otherwise the key is:
//
//	<cacheVersion>-cci_pycache[-<CIRCLE_BRANCH>][-<python --version output>]-<sha256(lockfile)>
//
// with the bracketed components included only when includeBranch/
// includePython are set, matching porting.md's cache key design.
func CacheKey(cacheVersion string, includeBranch, includePython bool, lockfilePath, override string) (string, error) {
	if override != "" {
		return override, nil
	}

	key := cacheVersion + "-" + cacheKeyTag

	if includeBranch {
		key += "-" + os.Getenv("CIRCLE_BRANCH")
	}

	if includePython {
		version, err := pythonVersion()
		if err != nil {
			return "", fmt.Errorf("resolve python version: %w", err)
		}
		key += "-" + version
	}

	// No {{ checksum }} templating: the function runs at cache-key-compute
	// time, not config-compile time, so it can just read the file itself.
	lockfile, err := os.ReadFile(lockfilePath) //nolint:gosec // lockfilePath is one of a fixed set of known filenames joined onto --app-dir
	if err != nil {
		return "", fmt.Errorf("read lockfile %s: %w", lockfilePath, err)
	}
	key += fmt.Sprintf("-%x", sha256.Sum256(lockfile))

	return key, nil
}

// pythonVersion returns the trimmed output of running `python --version`,
// falling back to `python3` if `python` isn't on PATH (see porting.md's
// "Python/pip binary resolution"). It's a variable rather than a plain
// function so tests can stub it out without depending on a real python
// interpreter being installed.
var pythonVersion = func() (string, error) {
	bin := "python"
	if _, err := exec.LookPath(bin); err != nil {
		bin = "python3"
	}

	// Python 2 writes its version to stderr; Python 3 writes it to
	// stdout. CombinedOutput catches whichever it is.
	out, err := exec.Command(bin, "--version").CombinedOutput() //nolint:gosec // bin is one of two fixed, known binary names
	if err != nil {
		return "", fmt.Errorf("run %s --version: %w", bin, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// RestoreCache restores the CircleCI job cache entry under key. It is
// best-effort: any failure -- including running outside a CircleCI job,
// where client returns sdk.ErrNoServer -- is logged to stderr rather than
// returned, mirroring setup-go/cache's Restore*Cache helpers.
func RestoreCache(ctx context.Context, client *sdk.Client, key string) {
	_, err := client.RestoreCache(ctx, []string{key})
	sdk.LogCacheErr("restore", err)
}

// SaveCache saves paths under key to the CircleCI job cache. Best-effort,
// same as RestoreCache.
func SaveCache(ctx context.Context, client *sdk.Client, key string, paths []string) {
	sdk.LogCacheErr("save", client.SaveCache(ctx, key, paths))
}
