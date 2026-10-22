package python

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/circleci-functions/toolkit/sdk"
)

// cacheKeyTag identifies setup-python's cache keys.
const cacheKeyTag = "cci_pycache"

// toolCacheDir returns the default cache directory for the tool named by
// unix (the directory name under the user cache dir on linux and darwin) and
// windows (the path segments under %LocalAppData%).
func toolCacheDir(unix string, windows ...string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache directory: %w", err)
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(append([]string{base}, windows...)...), nil
	}
	return filepath.Join(base, unix), nil
}

// VenvPaths returns the directories holding pm's virtual environment(s).
// appDir is the project directory, used for pipenv's in-project layout and
// uv's default. override, when non-empty, replaces the detected paths for
// any manager. Returns nil for pip and pip-dist, which install globally.
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
		if runtime.GOOS == "windows" {
			return []string{filepath.Join(home, ".virtualenvs")}, nil
		}
		return []string{filepath.Join(home, ".local", "share", "virtualenvs")}, nil
	case Poetry:
		dir, err := toolCacheDir("pypoetry", "pypoetry", "Cache")
		if err != nil {
			return nil, err
		}
		return []string{filepath.Join(dir, "virtualenvs")}, nil
	case UV:
		if wd := os.Getenv("CIRCLE_WORKING_DIRECTORY"); wd != "" {
			return []string{filepath.Join(wd, ".venv")}, nil
		}
		return []string{filepath.Join(appDir, ".venv")}, nil
	default:
		return nil, fmt.Errorf("unknown package manager %q", pm)
	}
}

// PypiPaths returns the directories holding pm's downloaded packages and
// installed site-packages.
func PypiPaths(pm PackageManager) ([]string, error) {
	pip, err := toolCacheDir("pip", "pip", "Cache")
	if err != nil {
		return nil, err
	}

	switch pm {
	case Pip, PipDist:
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		pyenv := filepath.Join(home, ".pyenv", "versions")
		userLib := filepath.Join(home, ".local", "lib")
		if runtime.GOOS == "windows" {
			pyenv = filepath.Join(home, ".pyenv", "pyenv-win", "versions")
			userLib = filepath.Join(os.Getenv("APPDATA"), "Python")
		}
		return []string{pip, pyenv, userLib}, nil
	case Pipenv:
		pipenv, err := toolCacheDir("pipenv", "pipenv", "Cache")
		if err != nil {
			return nil, err
		}
		return []string{pip, pipenv}, nil
	case Poetry:
		return []string{pip}, nil
	case UV:
		uv, err := toolCacheDir("uv", "uv", "cache")
		if err != nil {
			return nil, err
		}
		return []string{uv}, nil
	default:
		return nil, fmt.Errorf("unknown package manager %q", pm)
	}
}

// ErrNoLockfile is returned by CacheKey when the lockfile it hashes doesn't
// exist, which leaves nothing to key the cache on.
var ErrNoLockfile = errors.New("no lockfile")

// CacheKey returns the cache key for the venv/pypi cache buckets. A non-empty
// override is returned verbatim, without reading the lockfile. Otherwise the
// key is:
//
//	<cacheVersion>-cci_pycache[-<CIRCLE_BRANCH>][-<python --version output>]-<sha256(lockfile)>
//
// with the bracketed components included only when includeBranch and
// includePython are set.
func CacheKey(cacheVersion string, includeBranch, includePython bool, lockfilePath, override string) (string, error) {
	if override != "" {
		return override, nil
	}

	// No {{ checksum }} templating: the function runs at cache-key-compute
	// time, not config-compile time, so it can just read the file itself.
	lockfile, err := os.ReadFile(lockfilePath) //nolint:gosec // lockfilePath is one of a fixed set of known filenames joined onto --app-dir
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrNoLockfile, lockfilePath)
	}
	if err != nil {
		return "", fmt.Errorf("read lockfile %s: %w", lockfilePath, err)
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

	key += fmt.Sprintf("-%x", sha256.Sum256(lockfile))

	return key, nil
}

// pythonVersion returns the trimmed output of running python --version.
// It's a variable so tests can stub it without a real interpreter installed.
var pythonVersion = func() (string, error) {
	bin := ResolveBinaries().Python

	// Python 2 writes its version to stderr; Python 3 writes it to
	// stdout. CombinedOutput catches whichever it is.
	out, err := exec.Command(bin, "--version").CombinedOutput() //nolint:gosec // bin is one of two fixed, known binary names
	if err != nil {
		return "", fmt.Errorf("run %s --version: %w", bin, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// RestoreCache restores the CircleCI job cache entry under key. Failures,
// including running outside a CircleCI job, are logged rather than returned.
func RestoreCache(ctx context.Context, client *sdk.Client, key string) {
	_, err := client.RestoreCache(ctx, []string{key}, sdk.WithStepName("Restore Python package cache"))
	sdk.LogCacheErr("restore", err)
}

// SaveCache saves paths under key to the CircleCI job cache. Failures are
// logged rather than returned.
func SaveCache(ctx context.Context, client *sdk.Client, key string, paths []string) {
	sdk.LogCacheErr("save", client.SaveCache(ctx, key, paths, sdk.WithStepName("Save Python package cache")))
}
