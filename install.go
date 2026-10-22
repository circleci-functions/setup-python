package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/circleci-functions/toolkit/pathlink"
	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-python/python"
	"github.com/circleci-functions/setup-python/pythondist"
	"github.com/circleci-functions/setup-python/pythonpath"
	"github.com/circleci-functions/setup-python/pythonversion"
)

// liveDownloadsURL is where to look for a build the built-in list has none
// of. It's a variable so tests can point it at a fake server.
var liveDownloadsURL = pythondist.DefaultURL

// installCmd installs a relocatable CPython build for the running platform
// and makes python and pip resolvable by name for the rest of the job.
type installCmd struct {
	PythonVersion     string `help:"Python version to install: an exact or partial version (3.13, 3.13.1), a range (>=3.10 <3.14, ~=3.12), or x.y-dev to include prereleases. A t suffix (3.13t) installs a free-threaded build. Defaults to .python-version, then the Python already on PATH, then the newest stable release." example:"3.13"`
	PythonVersionFile string `help:"File to read the Python version from: .python-version, .tool-versions, pyproject.toml or Pipfile." type:"path"`
	AllowPrereleases  bool   `help:"Let a major.minor --python-version match prereleases when that version has no stable release yet."`
	Freethreaded      bool   `help:"Install a free-threaded build."`
	PipVersion        string `help:"Version of pip to install into the installed Python, e.g. 24.2."`
	Cache             bool   `help:"Restore the Python install from the job cache, and save it after a fresh download. Set to false to disable caching." default:"true"`
	InstallDir        string `help:"Directory to install Python versions under." default:"~/.cache/setup-python" hidden:""`
	BinDir            string `help:"Link python and pip into this directory instead of discovering one on PATH." type:"path" hidden:""`
	CacheNonce        string `help:"Partition the Python install cache, so a test run cannot restore an earlier run's install." hidden:""`
	DownloadsURL      string `help:"Read the list of Python builds from this URL instead of setup-python's built-in one." hidden:""`
}

func (c *installCmd) Run(ctx context.Context, client *sdk.Client) error {
	spec, ok, err := c.spec()
	if err != nil || !ok {
		return err
	}
	platform, err := pythondist.Host()
	if err != nil {
		return err
	}

	if err := c.checkCanLink(); err != nil {
		return err
	}

	version, root, err := c.ensureInstalled(ctx, client, spec, platform)
	if err != nil {
		return err
	}
	linked, err := pythonpath.Link(pythonpath.Options{Root: root, Version: version, BinDir: c.BinDir})
	if err != nil {
		return err
	}
	c.reportLinked(version, root, linked)

	if c.PipVersion != "" {
		if err := installPip(ctx, linked.Root, c.PipVersion); err != nil {
			return err
		}
	}
	if linked.Pyenv != "" {
		if err := pythonpath.RemoveUserInstalls(root); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "setup-python: pyenv rehashes after pip install, so the executables of what pip installs resolve by name\n")
		return nil
	}
	reportUserInstalls(root, linked.Dir)
	return nil
}

// reportLinked says where Link made the install at root resolvable from.
func (c *installCmd) reportLinked(version, root string, linked pythonpath.Result) {
	dir := linked.Dir
	fmt.Fprintf(os.Stderr, "setup-python: Python %s installed at %s, linked into %s\n", version, root, dir)
	if linked.Pyenv != "" {
		fmt.Fprintf(os.Stderr, "setup-python: registered Python %s with pyenv as %s, and made it pyenv's global version\n", version, linked.Pyenv)
	}
	if linked.Root != root {
		fmt.Fprintf(os.Stderr, "setup-python: pyenv runs its own Python %s from %s here, as a .python-version file or $PYENV_VERSION selects\n", version, linked.Root)
	}
	for _, d := range linked.PipDirs {
		fmt.Fprintf(os.Stderr, "setup-python: also linked pip into %s, where another pip came first on PATH\n", d)
	}
	if c.BinDir == "" {
		if other := pythonpath.Shadowed(dir, "pip"); other != "" && !slices.Contains(linked.PipDirs, other) {
			fmt.Fprintf(os.Stderr, "setup-python: warning: pip resolves from %s, ahead of %s; run python -m pip for the pip of the Python installed here\n", other, dir)
		}
	}
}

// checkCanLink reports an error when the step's environment cannot support
// linking Python onto PATH, so a job that could never use what it downloads
// fails before any download.
func (c *installCmd) checkCanLink() error {
	names := []string{"python", "python3"}
	if runtime.GOOS == "windows" {
		names = []string{"python"}
	}
	_, err := pathlink.Check(pathlink.Options{
		Names:     names,
		Preferred: pathlink.UserBinDirs(),
		Dir:       c.BinDir,
	})
	return err
}

// spec returns the version spec to install, or false when nothing asks for a
// version and the Python already on PATH is to be used instead.
func (c *installCmd) spec() (pythonversion.Spec, bool, error) {
	raw, err := c.versionSpec()
	if err != nil {
		return pythonversion.Spec{}, false, err
	}
	if raw == "" {
		if !c.Freethreaded && c.useSystemPython() {
			return pythonversion.Spec{}, false, nil
		}
		raw = pythonversion.DefaultSpec
	}
	spec, err := pythonversion.Parse(raw, c.Freethreaded, c.AllowPrereleases)
	return spec, err == nil, err
}

// ensureInstalled returns the version spec resolves to and the root of its
// install, restoring it from the job cache or downloading it unless an
// earlier run already did. An exact spec is resolved against the list of
// builds only when there is something to download.
func (c *installCmd) ensureInstalled(ctx context.Context, client *sdk.Client, spec pythonversion.Spec, platform pythondist.Platform) (version, root string, err error) {
	installDir, err := expandHome(c.InstallDir)
	if err != nil {
		return "", "", err
	}

	version, exact := spec.Exact()
	var build pythondist.Download
	if !exact {
		if build, err = c.resolve(ctx, spec, platform); err != nil {
			return "", "", err
		}
		version = build.Version()
	}
	root = filepath.Join(installDir, installName(version, spec.Freethreaded, platform))
	key := c.toolchainCacheKey(version, spec.Freethreaded, platform, root)
	if c.restore(ctx, client, key, version, root) {
		return version, root, nil
	}
	if exact {
		if build, err = c.resolve(ctx, spec, platform); err != nil {
			return "", "", err
		}
	}
	return version, root, c.download(ctx, client, key, build, root)
}

// useSystemPython reports whether a Python 3 is already on PATH, which is
// what to use when nothing asks for a version, linking python to python3 if
// only the latter resolves.
func (c *installCmd) useSystemPython() bool {
	version, bin, ok := systemPython()
	if !ok {
		fmt.Fprintf(os.Stderr, "setup-python: no --python-version, --python-version-file or %s, and no Python on PATH; installing the newest stable release\n", pythonversion.DefaultFile)
		return false
	}
	fmt.Fprintf(os.Stderr, "setup-python: no --python-version, --python-version-file or %s; using %s at %s\n", pythonversion.DefaultFile, version, bin)
	if c.PipVersion != "" {
		fmt.Fprintf(os.Stderr, "setup-python: warning: ignoring --pip-version, which only applies to a Python setup-python installs\n")
	}
	if err := python.EnsureAliased(python.ResolveBinaries()); err != nil {
		fmt.Fprintf(os.Stderr, "setup-python: warning: link python to python3: %v\n", err)
	}
	return true
}

// systemPython returns the version and path of the Python 3 on PATH, if any.
// A python that fails to run, such as the Windows stub that offers to install
// Python from the Microsoft Store, does not count.
func systemPython() (version, bin string, ok bool) {
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		out, err := exec.Command(path, "--version").CombinedOutput() //nolint:gosec // one of two fixed names, resolved on PATH
		if v := strings.TrimSpace(string(out)); err == nil && strings.HasPrefix(v, "Python 3") {
			return v, path, true
		}
	}
	return "", "", false
}

// versionSpec picks the version spec from --python-version,
// --python-version-file, or .python-version in the working directory, in that
// order. It returns the empty string when there is none of them.
func (c *installCmd) versionSpec() (string, error) {
	switch {
	case c.PythonVersion != "":
		if c.PythonVersionFile != "" {
			fmt.Fprintf(os.Stderr, "setup-python: both --python-version and --python-version-file are set; using --python-version\n")
		}
		return c.PythonVersion, nil
	case c.PythonVersionFile != "":
		spec, err := pythonversion.FromFile(c.PythonVersionFile)
		if err != nil {
			return "", fmt.Errorf("python-version-file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "setup-python: read Python version %q from %s\n", spec, c.PythonVersionFile)
		return spec, nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	spec, source, err := pythonversion.Default(wd)
	if err != nil {
		return "", err
	}
	if source != "" {
		fmt.Fprintf(os.Stderr, "setup-python: read Python version %q from %s\n", spec, source)
	}
	return spec, nil
}

// resolve returns the build of the newest release matching spec for platform.
// It looks in --downloads-url when that is set, and otherwise in the list
// built into setup-python, checking uv's current list only when the built-in
// one has no match: a release newer than this setup-python.
func (c *installCmd) resolve(ctx context.Context, spec pythonversion.Spec, platform pythondist.Platform) (pythondist.Download, error) {
	if c.DownloadsURL != "" {
		catalog, err := pythondist.Fetch(ctx, c.DownloadsURL)
		if err != nil {
			return pythondist.Download{}, err
		}
		return pick(catalog, spec, platform)
	}

	catalog, err := pythondist.Snapshot()
	if err != nil {
		return pythondist.Download{}, err
	}
	build, err := pick(catalog, spec, platform)
	if !errors.Is(err, pythonversion.ErrNoMatch) {
		return build, err
	}
	fmt.Fprintf(os.Stderr, "setup-python: %v in setup-python's built-in list; checking uv's current list\n", err)
	live, ferr := pythondist.Fetch(ctx, liveDownloadsURL)
	if ferr != nil {
		return pythondist.Download{}, fmt.Errorf("%w, and could not check uv's current list: %w", err, ferr)
	}
	return pick(live, spec, platform)
}

// pick returns the build in catalog of the newest release matching spec for
// platform.
func pick(catalog pythondist.Catalog, spec pythonversion.Spec, platform pythondist.Platform) (pythondist.Download, error) {
	builds := catalog.For(platform, spec.Freethreaded)
	version, err := spec.Resolve(builds.Versions())
	if err != nil {
		return pythondist.Download{}, fmt.Errorf("%w for %s", err, describe(platform, spec.Freethreaded))
	}
	build, _ := builds.Find(version)
	fmt.Fprintf(os.Stderr, "setup-python: resolved %q to Python %s (%s)\n", spec, version, describe(platform, spec.Freethreaded))
	return build, nil
}

func describe(platform pythondist.Platform, freethreaded bool) string {
	if freethreaded {
		return platform.String() + ", free-threaded"
	}
	return platform.String()
}

// restore reports whether root holds the install of version, from an earlier
// run or, failing that, the job cache. A restore that fails part way is
// removed rather than trusted.
func (c *installCmd) restore(ctx context.Context, client *sdk.Client, key, version, root string) bool {
	if pythonpath.Installed(root) {
		return true
	}
	if !c.Cache {
		return false
	}
	_, err := client.RestoreCache(ctx, []string{key}, sdk.WithStepName("Restoring Python "+version))
	sdk.LogCacheErr("restore", err)
	if err != nil {
		_ = os.RemoveAll(root)
		return false
	}
	if !pythonpath.Installed(root) {
		return false
	}
	fmt.Fprintf(os.Stderr, "setup-python: restored Python %s from cache\n", version)
	return true
}

// download installs build at root, and saves it to the job cache.
func (c *installCmd) download(ctx context.Context, client *sdk.Client, key string, build pythondist.Download, root string) error {
	if err := pythondist.Install(ctx, build, root); err != nil {
		return err
	}
	if c.Cache {
		sdk.LogCacheErr("save", client.SaveCache(ctx, key, []string{root}, sdk.WithStepName("Saving Python "+build.Version())))
	}
	return nil
}

// toolchainCacheKey returns the key the install at root is cached under. The
// cache restores to the absolute path it saved, so the key carries a hash of
// root: executors whose $HOME differs must not share an entry that restores
// somewhere only one of them looks.
func (c *installCmd) toolchainCacheKey(version string, freethreaded bool, platform pythondist.Platform, root string) string {
	sum := sha256.Sum256([]byte(root))
	key := fmt.Sprintf("setup-python-toolchain-v1-%s-%x", installName(version, freethreaded, platform), sum[:4])
	if c.CacheNonce != "" {
		key += "-" + c.CacheNonce
	}
	return key
}

// installName names the directory an install lives in, e.g.
// 3.13.1-linux-x86_64-gnu or 3.13.1t-darwin-aarch64.
func installName(version string, freethreaded bool, platform pythondist.Platform) string {
	if freethreaded {
		version += "t"
	}
	return version + "-" + platform.String()
}

// installPip installs the given version of pip into the install at root.
// --no-user, because the install's pip config may say to install for the user.
func installPip(ctx context.Context, root, version string) error {
	//nolint:gosec // the install's own python
	cmd := exec.CommandContext(ctx, pythonpath.Executable(root), "-m", "pip", "install", "--no-user", "--disable-pip-version-check", "pip=="+version)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install pip %s: %w", version, err)
	}
	return nil
}

// reportUserInstalls has pip install packages for the user when that puts
// their executables beside python on PATH, and says where they go either way.
func reportUserInstalls(root, linkedDir string) {
	scripts, err := pythonpath.UserInstalls(root, linkedDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup-python: warning: %v; executables from pip install land in %s, which is not on PATH, so run them with python -m\n", err, pythonpath.ScriptsDir(root))
		return
	}
	fmt.Fprintf(os.Stderr, "setup-python: pip installs packages for the user, so their executables land in %s (pass --no-user to pip to install into the Python itself)\n", scripts)
}

// expandHome resolves a leading ~ against $HOME. kong's path type resolves it
// against the passwd entry instead, which ignores a redirected $HOME.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}
