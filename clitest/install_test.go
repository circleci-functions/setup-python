package clitest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"

	"github.com/circleci-functions/setup-python/internal/testing/fakedist"
	"github.com/circleci-functions/setup-python/pythondist"
)

var releases = []fakedist.Release{
	{Version: "3.10.12", NoBinPython: true},
	{Version: "3.12.4"},
	{Version: "3.13.0"},
	{Version: "3.13.1"},
	{Version: "3.13.1", Freethreaded: true},
	{Version: "3.14.0rc1"},
}

// installEnv starts a fake download server and returns it with a bin dir to
// link into. The fake builds' python is a shell script, so these tests need a
// Unix.
func installEnv(t *testing.T) (*fakedist.Server, string) {
	t.Helper()
	skip.If(t, runtime.GOOS == "windows", "the fake Python builds are shell scripts")
	return fakedist.New(t, releases...), t.TempDir()
}

// installArgs are the flags that point an install at srv and binDir.
func installArgs(srv *fakedist.Server, binDir string, args ...string) []string {
	return append(args, "--downloads-url", srv.MetadataURL(), "--bin-dir", binDir)
}

// runIn runs the binary in s's environment with dir as its working directory.
func runIn(t *testing.T, s *sdktest.Sandbox, dir string, extraEnv map[string]string, args ...string) (string, error) {
	t.Helper()
	r := runner.New(t, binaryPath, runner.WithSandboxEnv(sandboxEnv(s, extraEnv)), runner.WithCmdDir(dir))
	_, err := r.Run(args...)
	return r.StdoutString() + r.StderrString(), err
}

func pythonVersion(t *testing.T, binDir string) string {
	t.Helper()
	out, err := exec.Command(filepath.Join(binDir, "python"), "--version").Output()
	assert.NilError(t, err)
	return strings.TrimSpace(string(out))
}

func host(t *testing.T) pythondist.Platform {
	t.Helper()
	p, err := pythondist.Host()
	assert.NilError(t, err)
	return p
}

func TestCLI_Install_ResolvesVersion(t *testing.T) {
	appDir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(appDir, "pyproject.toml"), []byte("[project]\nrequires-python = \">=3.12,<3.13\"\n"), 0600))

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "minor", args: []string{"--python-version", "3.13"}, want: "Python 3.13.1"},
		{name: "exact", args: []string{"--python-version", "3.13.0"}, want: "Python 3.13.0"},
		{name: "build without bin/python", args: []string{"--python-version", "3.10.12"}, want: "Python 3.10.12"},
		{name: "range", args: []string{"--python-version", ">=3.12 <3.13"}, want: "Python 3.12.4"},
		{name: "newest", args: []string{"--python-version", "3.x"}, want: "Python 3.13.1"},
		{name: "prerelease fallback", args: []string{"--python-version", "3.14", "--allow-prereleases"}, want: "Python 3.14.0rc1"},
		{name: "dev", args: []string{"--python-version", "3.14-dev"}, want: "Python 3.14.0rc1"},
		{name: "exact prerelease", args: []string{"--python-version", "3.14.0rc1"}, want: "Python 3.14.0rc1"},
		{name: "version file", args: []string{"--python-version-file", filepath.Join(appDir, "pyproject.toml")}, want: "Python 3.12.4"},
		{name: "explicit install command", args: []string{"install", "--python-version", "3.12"}, want: "Python 3.12.4"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, binDir := installEnv(t)
			_, run := sandbox(t)

			out, err := run(t, nil, installArgs(srv, binDir, tc.args...)...)
			assert.NilError(t, err, "output: %s", out)
			assert.Equal(t, pythonVersion(t, binDir), tc.want)
		})
	}
}

func TestCLI_Install_VersionFlagWinsOverFile(t *testing.T) {
	srv, binDir := installEnv(t)
	_, run := sandbox(t)
	file := filepath.Join(t.TempDir(), ".python-version")
	assert.NilError(t, os.WriteFile(file, []byte("3.13\n"), 0600))

	out, err := run(t, nil, installArgs(srv, binDir, "--python-version", "3.12", "--python-version-file", file)...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, pythonVersion(t, binDir), "Python 3.12.4")
	assert.Assert(t, strings.Contains(out, "using --python-version"), "expected a warning; got: %s", out)
}

func TestCLI_Install_DefaultVersion(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "python-version", files: map[string]string{".python-version": "3.12\n"}, want: "Python 3.12.4"},
		{name: "newest stable without a Python on PATH", want: "Python 3.13.1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, binDir := installEnv(t)
			s := sdktest.NewSandbox(t)
			dir := t.TempDir()
			for name, content := range tc.files {
				assert.NilError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
			}

			out, err := runIn(t, s, dir, map[string]string{"PATH": t.TempDir()}, installArgs(srv, binDir)...)
			assert.NilError(t, err, "output: %s", out)
			assert.Equal(t, pythonVersion(t, binDir), tc.want)
		})
	}
}

// TestCLI_Install_UsesThePythonOnPath checks that with no version asked for, a
// Python 3 already on PATH is used as it is, and nothing is downloaded.
func TestCLI_Install_UsesThePythonOnPath(t *testing.T) {
	srv, binDir := installEnv(t)
	s := sdktest.NewSandbox(t)
	pathDir := t.TempDir()
	//nolint:gosec // test stub; 0700 so it is executable
	assert.NilError(t, os.WriteFile(filepath.Join(pathDir, "python3"), []byte("#!/bin/sh\necho 'Python 3.11.2'\n"), 0700))

	out, err := runIn(t, s, t.TempDir(), map[string]string{"PATH": pathDir}, installArgs(srv, binDir, "--pip-version", "24.2")...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "using Python 3.11.2 at "+filepath.Join(pathDir, "python3")), "output: %s", out)
	assert.Assert(t, strings.Contains(out, "ignoring --pip-version"), "output: %s", out)
	assert.Equal(t, srv.MetadataFetches(), 0)
	assert.Equal(t, srv.Downloads(), 0)
	entries, err := os.ReadDir(binDir)
	assert.NilError(t, err)
	assert.Equal(t, len(entries), 0, "nothing should be linked into --bin-dir: %v", entries)
}

func TestCLI_Install_FreethreadedWithoutAVersion(t *testing.T) {
	srv, binDir := installEnv(t)
	s := sdktest.NewSandbox(t)
	pathDir := t.TempDir()
	//nolint:gosec // test stub; 0700 so it is executable
	assert.NilError(t, os.WriteFile(filepath.Join(pathDir, "python3"), []byte("#!/bin/sh\necho 'Python 3.11.2'\n"), 0700))

	out, err := runIn(t, s, t.TempDir(), map[string]string{"PATH": pathDir}, installArgs(srv, binDir, "--freethreaded")...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, pythonVersion(t, binDir), "Python 3.13.1")
	_, err = os.Stat(filepath.Join(s.Home, ".cache", "setup-python", "3.13.1t-"+host(t).String()))
	assert.NilError(t, err, "expected the free-threaded build; output: %s", out)
}

func TestCLI_Install_Freethreaded(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "t suffix", args: []string{"--python-version", "3.13t"}},
		{name: "flag", args: []string{"--python-version", "3.13", "--freethreaded"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, binDir := installEnv(t)
			s, run := sandbox(t)

			out, err := run(t, nil, installArgs(srv, binDir, tc.args...)...)
			assert.NilError(t, err, "output: %s", out)
			root := filepath.Join(s.Home, ".cache", "setup-python", "3.13.1t-"+host(t).String())
			target, err := filepath.EvalSymlinks(filepath.Join(binDir, "python"))
			assert.NilError(t, err)
			assert.Equal(t, target, filepath.Join(realPath(t, root), "bin", "python3.13t"))
		})
	}
}

func TestCLI_Install_LinksTools(t *testing.T) {
	srv, binDir := installEnv(t)
	_, run := sandbox(t)

	out, err := run(t, nil, installArgs(srv, binDir, "--python-version", "3.13")...)
	assert.NilError(t, err, "output: %s", out)
	for _, tool := range []string{"python", "python3", "python3.13", "pip", "pip3", "pip3.13"} {
		_, err := os.Stat(filepath.Join(binDir, tool))
		assert.NilError(t, err, "%s not linked", tool)
	}
}

// TestCLI_Install_UserInstalls checks pip is told to install for the user
// exactly when that puts the executables of what it installs on PATH beside
// python: the fake python reports $HOME/.local/bin as pip's user scripts
// directory, as a real one does on Linux and macOS.
func TestCLI_Install_UserInstalls(t *testing.T) {
	tests := []struct {
		name     string
		binDir   func(s *sdktest.Sandbox) string
		wantConf bool
		wantOut  string
	}{
		{
			name:     "linked into the user scripts directory",
			binDir:   func(s *sdktest.Sandbox) string { return filepath.Join(s.Home, ".local", "bin") },
			wantConf: true,
			wantOut:  "pip installs packages for the user",
		},
		{
			name:    "linked elsewhere",
			binDir:  func(*sdktest.Sandbox) string { return t.TempDir() },
			wantOut: "which is not on PATH, so run them with python -m",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := installEnv(t)
			s, run := sandbox(t)
			binDir := tc.binDir(s)

			out, err := run(t, nil, installArgs(srv, binDir, "--python-version", "3.13")...)
			assert.NilError(t, err, "output: %s", out)
			assert.Assert(t, strings.Contains(out, tc.wantOut), "output: %s", out)

			root := filepath.Join(s.Home, ".cache", "setup-python", "3.13.1-"+host(t).String())
			_, err = os.Stat(filepath.Join(root, "pip.conf"))
			assert.Equal(t, err == nil, tc.wantConf, "pip.conf: %v", err)
		})
	}
}

func TestCLI_Install_PipVersion(t *testing.T) {
	srv, binDir := installEnv(t)
	_, run := sandbox(t)
	logPath := filepath.Join(t.TempDir(), "stub.log")

	out, err := run(t, map[string]string{"STUB_LOG": logPath}, installArgs(srv, binDir, "--python-version", "3.13", "--pip-version", "24.2")...)
	assert.NilError(t, err, "output: %s", out)
	calls, err := os.ReadFile(logPath)
	assert.NilError(t, err)
	assert.Equal(t, strings.TrimSpace(string(calls)), "python -m pip install --no-user --disable-pip-version-check pip==24.2")
}

func TestCLI_Install_Cache(t *testing.T) {
	name := "3.13.1-" + host(t).String()

	srv, binDir := installEnv(t)
	s, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)
	root := filepath.Join(s.Home, ".cache", "setup-python", name)
	wantKey := "setup-python-toolchain-v1-" + name + "-" + rootHash(root)

	out, err := run(t, map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL}, installArgs(srv, binDir, "--python-version", "3.13")...)
	assert.NilError(t, err, "output: %s", out)

	assert.Equal(t, len(rec.restores), 1)
	assert.DeepEqual(t, rec.restores[0].Keys, []string{wantKey})
	assert.Equal(t, rec.restores[0].Name, "Restoring Python 3.13.1")
	assert.Equal(t, len(rec.saves), 1)
	assert.Equal(t, rec.saves[0].Key, wantKey)
	assert.DeepEqual(t, rec.saves[0].Paths, []string{root})
	assert.Equal(t, rec.saves[0].Name, "Saving Python 3.13.1")
}

func TestCLI_Install_CacheNonce(t *testing.T) {
	srv, binDir := installEnv(t)
	s, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)

	out, err := run(t, map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL}, installArgs(srv, binDir, "--python-version", "3.13", "--cache-nonce", "run-42")...)
	assert.NilError(t, err, "output: %s", out)

	name := "3.13.1-" + host(t).String()
	wantKey := "setup-python-toolchain-v1-" + name + "-" + rootHash(filepath.Join(s.Home, ".cache", "setup-python", name)) + "-run-42"
	assert.DeepEqual(t, rec.restores[0].Keys, []string{wantKey})
	assert.Equal(t, rec.saves[0].Key, wantKey)
}

func TestCLI_Install_CacheDisabled(t *testing.T) {
	srv, binDir := installEnv(t)
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)

	out, err := run(t, map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL}, installArgs(srv, binDir, "--python-version", "3.13", "--cache=false")...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, len(rec.restores), 0)
	assert.Equal(t, len(rec.saves), 0)
	assert.Equal(t, srv.Downloads(), 1)
}

func TestCLI_Install_RestoresFromCache(t *testing.T) {
	srv, binDir := installEnv(t)
	s, run := sandbox(t)
	args := installArgs(srv, binDir, "--python-version", "3.13")

	out, err := run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, srv.Downloads(), 1)

	assert.NilError(t, os.RemoveAll(filepath.Join(s.Home, ".cache", "setup-python")))
	out, err = run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, srv.Downloads(), 1, "expected the second run to restore rather than download")
	assert.Assert(t, strings.Contains(out, "restored Python 3.13.1 from cache"), "output: %s", out)
	assert.Equal(t, pythonVersion(t, binDir), "Python 3.13.1")
}

func TestCLI_Install_RerunReusesInstall(t *testing.T) {
	srv, binDir := installEnv(t)
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)
	env := map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL}
	args := installArgs(srv, binDir, "--python-version", "3.13")

	for range 2 {
		out, err := run(t, env, args...)
		assert.NilError(t, err, "output: %s", out)
	}
	assert.Equal(t, srv.Downloads(), 1)
	assert.Equal(t, len(rec.restores), 1, "a rerun should not touch the cache")
}

func TestCLI_Install_ExactVersionNeedsNoListWhenAvailable(t *testing.T) {
	srv, binDir := installEnv(t)
	s, run := sandbox(t)
	args := installArgs(srv, binDir, "--python-version", "3.13.1")

	out, err := run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, srv.MetadataFetches(), 1, "expected the first run to look the build up")
	served := len(srv.Requests())

	out, err = run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, len(srv.Requests()), served, "expected a rerun to use the install without the server")

	assert.NilError(t, os.RemoveAll(filepath.Join(s.Home, ".cache", "setup-python")))
	out, err = run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "restored Python 3.13.1 from cache"), "output: %s", out)
	assert.Equal(t, len(srv.Requests()), served, "expected a cache restore without the server")
	assert.Equal(t, pythonVersion(t, binDir), "Python 3.13.1")
}

func TestCLI_Install_Errors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		corrupt bool
		wantErr string
	}{
		{name: "no matching version", args: []string{"--python-version", "3.11"}, wantErr: `no Python release matches "3.11" for ` + host(t).String()},
		{name: "no stable release", args: []string{"--python-version", "3.14"}, wantErr: `no Python release matches "3.14"`},
		{name: "no free-threaded build", args: []string{"--python-version", "3.12t"}, wantErr: `no Python release matches "3.12t" for ` + host(t).String() + ", free-threaded"},
		{name: "invalid version", args: []string{"--python-version", "banana"}, wantErr: `invalid Python version spec "banana"`},
		{name: "pypy", args: []string{"--python-version", "pypy3.10"}, wantErr: "setup-python installs CPython only"},
		{name: "missing version file", args: []string{"--python-version-file", "/nonexistent/.python-version"}, wantErr: "python-version-file"},
		{name: "checksum mismatch", args: []string{"--python-version", "3.13"}, corrupt: true, wantErr: "checksum mismatch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, binDir := installEnv(t)
			if tc.corrupt {
				srv.Corrupt()
			}
			_, run := sandbox(t)

			out, err := run(t, nil, installArgs(srv, binDir, tc.args...)...)
			assert.Assert(t, err != nil, "expected a non-zero exit; output: %s", out)
			assert.Assert(t, strings.Contains(out, tc.wantErr), "expected %q in output: %s", tc.wantErr, out)
		})
	}
}

// rootHash is the part of the cache key that identifies where the install
// lives.
func rootHash(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:4])
}

// realPath resolves symlinks, so a macOS /var answer compares equal to the
// /private/var it points at.
func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	assert.NilError(t, err)
	return r
}
