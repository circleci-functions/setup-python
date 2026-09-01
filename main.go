// Command setup-python installs Python packages using the detected (or
// explicitly selected) package manager, restoring/saving its venv and pypi
// cache directories via the CircleCI job cache around the install.
//
// Supported platforms: linux, darwin, and windows.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-python/python"
)

type cli struct {
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
func (c *cli) Run(ctx context.Context, client *sdk.Client) error {
	if err := c.resolveAppDir(); err != nil {
		return err
	}

	binaries := python.ResolveBinaries()
	if err := python.EnsureAliased(binaries); err != nil {
		return fmt.Errorf("write python/pip alias: %w", err)
	}

	pm, err := c.resolvePackageManager()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "setup-python: using package manager %s\n", pm)

	paths, err := c.cachePaths(pm)
	if err != nil {
		return err
	}

	cacheKey, err := c.restoreCache(ctx, client, pm, paths)
	if err != nil {
		return err
	}

	if err := c.installPackages(ctx, pm, binaries); err != nil {
		return err
	}

	if cacheKey != "" {
		python.SaveCache(ctx, client, cacheKey, paths)
	}

	return nil
}

// resolveAppDir replaces c.AppDir with its absolute form, erroring if it
// doesn't exist or isn't a directory.
func (c *cli) resolveAppDir() error {
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
func (c *cli) cachePaths(pm python.PackageManager) ([]string, error) {
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

// restoreCache computes the cache key and restores paths under it, returning
// the empty string (telling the caller to skip the eventual save) when
// caching is disabled or there's nothing to cache (both --pypi-cache=false and
// --venv-cache=false).
func (c *cli) restoreCache(ctx context.Context, client *sdk.Client, pm python.PackageManager, paths []string) (string, error) {
	if !c.Cache || len(paths) == 0 {
		return "", nil
	}

	lockfilePath := filepath.Join(c.AppDir, lockfileName(pm, c.PipDependencyFile))
	cacheKey, err := python.CacheKey(c.CacheVersion, c.IncludeBranchInCacheKey, c.IncludePythonInCacheKey, lockfilePath, c.CacheKeyOverride)
	if err != nil {
		return "", fmt.Errorf("compute cache key: %w", err)
	}
	python.RestoreCache(ctx, client, cacheKey)
	return cacheKey, nil
}

// installPackages runs pm's install command in c.AppDir. pip and pip-dist
// use the resolved pip binary; every other manager uses its own name.
func (c *cli) installPackages(ctx context.Context, pm python.PackageManager, binaries python.Binaries) error {
	installBin := pm.Binary()
	if pm == python.Pip || pm == python.PipDist {
		installBin = binaries.Pip
	}
	args := pm.InstallArgs(c.PipDependencyFile, c.PathArgs, splitArgs(c.Args))
	return runInstall(ctx, installBin, args, c.AppDir)
}

// resolvePackageManager returns the package manager from the flag, or by
// detecting it from files in AppDir when the flag is "auto".
func (c *cli) resolvePackageManager() (python.PackageManager, error) {
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

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-python"),
		kong.Description("Install Python packages using the detected or specified package manager, restoring/saving its venv and pypi caches around the install.\n\nExample:\n\n    circleci run setup-python@<version> -- --app-dir ./service"),
		kong.UsageOnError(),
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	return kCtx.Run(ctx)
}
