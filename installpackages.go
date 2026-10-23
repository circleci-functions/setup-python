package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-python/python"
)

// installPackagesCmd installs a project's dependencies with its package
// manager, restoring and saving the package manager's venv and pypi caches
// around the install so later steps start warm.
type installPackagesCmd struct {
	AppDir                  string   `help:"Directory containing the Python project." default:"."`
	Args                    string   `help:"Extra arguments appended to the install command." default:""`
	CacheKeyOverride        string   `help:"Use this value verbatim as the cache key instead of the computed one." default:""`
	CacheVersion            string   `help:"Leading component of the computed cache key; bump to force a cache miss." default:"v1"`
	IncludeBranchInCacheKey bool     `help:"Include $CIRCLE_BRANCH in the computed cache key." default:"true"`
	IncludePythonInCacheKey bool     `help:"Include the resolved python --version output in the computed cache key." default:"true"`
	PathArgs                string   `help:"Path passed to 'pip install -e' for package-manager=pip-dist." default:"."`
	PipDependencyFile       string   `help:"pip/pip-dist dependency file; also doubles as their cache-key lockfile." default:"requirements.txt"`
	PackageManager          string   `help:"Package manager to use. Auto-detected from project files when 'auto'." enum:"auto,poetry,pipenv,pip,pip-dist,uv" default:"auto"`
	Cache                   bool     `help:"Restore the venv/pypi caches before install and save them afterwards. Set to false to disable caching." default:"true"`
	PypiCache               bool     `help:"Include the pypi package/download cache in --cache's restore/save, when --cache is true." default:"true"`
	VenvCache               bool     `help:"Include the venv cache in --cache's restore/save, when --cache is true. No-op for pip/pip-dist, which have no venv." default:"true"`
	VenvPath                []string `help:"Override the detected venv path(s) to cache. Repeatable."`
}

// Run resolves the package manager, restores its cache (unless --cache=false),
// runs its install command, and saves the cache back.
func (c *installPackagesCmd) Run(ctx context.Context, client *sdk.Client) error {
	if err := c.resolveAppDir(); err != nil {
		return err
	}

	binaries := python.ResolveBinaries()
	linkAliases(binaries)

	pm, err := c.resolvePackageManager()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "setup-python: using package manager %s\n", pm)

	cacheKey, paths := c.restoreCache(ctx, client, pm)

	if err := c.installPackages(ctx, pm, binaries); err != nil {
		return err
	}

	if cacheKey != "" {
		python.SaveCache(ctx, client, cacheKey, paths)
	}

	return nil
}

// linkAliases links python and pip to python3 and pip3 when only those
// resolve, so later steps can run them by name. A failure is only a warning:
// setup-python itself runs python3 and pip3 directly.
func linkAliases(binaries python.Binaries) {
	if err := python.EnsureAliased(binaries); err != nil {
		fmt.Fprintf(os.Stderr, "setup-python: warning: link python to python3: %v\n", err)
	}
}

// resolveAppDir replaces c.AppDir with its absolute form, erroring if it
// doesn't exist or isn't a directory.
func (c *installPackagesCmd) resolveAppDir() error {
	appDir, err := filepath.Abs(c.AppDir)
	if err != nil {
		return fmt.Errorf("resolve app-dir: %w", err)
	}
	info, err := os.Stat(appDir)
	if err != nil {
		return fmt.Errorf("app-dir %q: %w", appDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("app-dir %q is not a directory", appDir)
	}
	c.AppDir = appDir
	return nil
}

// cachePaths returns the venv and/or pypi cache paths for pm, per whether
// --venv-cache/--pypi-cache are set.
func (c *installPackagesCmd) cachePaths(pm python.PackageManager) ([]string, error) {
	var paths []string
	if c.VenvCache {
		venvPaths, err := python.VenvPaths(pm, c.AppDir, c.VenvPath)
		if err != nil {
			return nil, fmt.Errorf("resolve venv cache paths: %w", err)
		}
		paths = append(paths, venvPaths...)
	}
	if c.PypiCache {
		pypiPaths, err := python.PypiPaths(pm)
		if err != nil {
			return nil, fmt.Errorf("resolve pypi cache paths: %w", err)
		}
		paths = append(paths, pypiPaths...)
	}
	return paths, nil
}

// restoreCache computes the cache key and restores the cache under it,
// returning the key and the paths to save under it afterwards. It returns an
// empty key, telling the caller to skip the save, when caching is disabled,
// there's nothing to cache (both --pypi-cache=false and --venv-cache=false),
// or the key can't be computed: a cache problem never fails the job.
func (c *installPackagesCmd) restoreCache(ctx context.Context, client *sdk.Client, pm python.PackageManager) (string, []string) {
	if !c.Cache {
		return "", nil
	}
	paths, err := c.cachePaths(pm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup-python: warning: %v; skipping cache\n", err)
		return "", nil
	}
	if len(paths) == 0 {
		return "", nil
	}

	lockfile := lockfileName(pm, c.PipDependencyFile)
	cacheKey, err := python.CacheKey(c.CacheVersion, c.IncludeBranchInCacheKey, c.IncludePythonInCacheKey, filepath.Join(c.AppDir, lockfile), c.CacheKeyOverride)
	if errors.Is(err, python.ErrNoLockfile) {
		fmt.Fprintf(os.Stderr, "setup-python: no %s in %s, skipping cache\n", lockfile, c.AppDir)
		return "", nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup-python: warning: compute cache key: %v; skipping cache\n", err)
		return "", nil
	}
	python.RestoreCache(ctx, client, cacheKey)
	return cacheKey, paths
}

// installPackages runs pm's install command in c.AppDir. pip and pip-dist
// use the resolved pip binary; every other manager uses its own name.
func (c *installPackagesCmd) installPackages(ctx context.Context, pm python.PackageManager, binaries python.Binaries) error {
	installBin := pm.Binary()
	if pm == python.Pip || pm == python.PipDist {
		installBin = binaries.Pip
	} else if err := ensurePackageManager(ctx, pm, binaries, c.AppDir); err != nil {
		return err
	}
	args := pm.InstallArgs(c.PipDependencyFile, c.PathArgs, splitArgs(c.Args))
	return runInstall(ctx, installBin, args, c.AppDir)
}

// ensurePackageManager installs pm with pip when it isn't on PATH, at the
// version the project pins, if any.
func ensurePackageManager(ctx context.Context, pm python.PackageManager, binaries python.Binaries, appDir string) error {
	if _, err := exec.LookPath(pm.Binary()); err == nil {
		return nil
	}
	requirement := pm.Requirement(appDir)
	fmt.Fprintf(os.Stderr, "setup-python: %s is not on PATH; installing %s with %s\n", pm, requirement, binaries.Pip)
	if err := runInstall(ctx, binaries.Pip, []string{"install", requirement}, appDir); err != nil {
		return err
	}
	if _, err := exec.LookPath(pm.Binary()); err != nil {
		return fmt.Errorf("installed %s, but %s is still not on PATH; put pip's scripts directory on PATH, or install %s in an earlier step", requirement, pm, pm)
	}
	return nil
}

// resolvePackageManager returns the package manager from the flag, or by
// detecting it from files in AppDir when the flag is "auto".
func (c *installPackagesCmd) resolvePackageManager() (python.PackageManager, error) {
	if c.PackageManager != "auto" {
		return python.PackageManager(c.PackageManager), nil
	}
	pm, err := python.DetectFromDir(c.AppDir, "")
	if err != nil {
		return "", fmt.Errorf("%w; use --package-manager to specify one explicitly", err)
	}
	return pm, nil
}

// lockfileName returns the file, relative to --app-dir, to hash for the
// cache key. pip and pip-dist have no lockfile format, so their dependency
// file stands in for one.
func lockfileName(pm python.PackageManager, pipDependencyFile string) string {
	switch pm {
	case python.Pip, python.PipDist:
		return pipDependencyFile
	default:
		return pm.Lockfile()
	}
}

// splitArgs shell-splits the --args flag's value into argv fields, appended
// verbatim to every package manager's install command.
func splitArgs(args string) []string {
	if args == "" {
		return nil
	}
	return strings.Fields(args)
}

// runInstall executes bin with args in appDir, forwarding stdout and stderr.
// It's a variable so tests can stub it without a real package manager
// installed.
var runInstall = func(ctx context.Context, bin string, args []string, appDir string) error {
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("%s binary not found on PATH", bin)
	}

	//nolint:gosec // bin is one of a fixed, known set of binary names
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = appDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s install: %w", bin, err)
	}
	return nil
}
