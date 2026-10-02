//go:build acceptance

package setuppython_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/sdk"
	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"
)

// acceptanceVersion returns the Python version spec to install. CI sets
// SETUP_PYTHON_ACCEPTANCE_VERSION from the pipeline's python-version
// parameter, so every job in a pipeline installs the same release.
func acceptanceVersion() string {
	if v := os.Getenv("SETUP_PYTHON_ACCEPTANCE_VERSION"); v != "" {
		return v
	}
	return "3.13"
}

var exactVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// TestAcceptance_InstallsOntoTheRealPath is the one test that lets
// setup-python choose where python is linked, rather than telling it with
// --bin-dir. That choice depends on the executor, and a sandboxed suite never
// reaches it.
//
// It changes the machine it runs on, up to taking over a Python already
// there, so it runs only where that is disposable: a CircleCI job, or a
// container the caller vouches for with $SETUP_PYTHON_DISPOSABLE (see
// `task smoke-local`).
func TestAcceptance_InstallsOntoTheRealPath(t *testing.T) {
	if os.Getenv("SETUP_PYTHON_DISPOSABLE") == "" {
		sdktest.SkipUnlessInJob(t)
	}

	s := sdktest.NewSandbox(t, sdktest.WithLiveCache())
	spec := acceptanceVersion()

	bashEnv := filepath.Join(t.TempDir(), "bash_env")
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		opts := []runner.Opt{
			runner.WithUnsafeOSEnvVars(),
			runner.WithUnsafeAdditionalEnvVars(map[string]string{"BASH_ENV": bashEnv}),
		}
		if dir != "" {
			opts = append(opts, runner.WithCmdDir(dir))
		}
		r := runner.New(t, binaryPath, opts...)
		_, err := r.Run(args...)
		out := r.StdoutString() + r.StderrString()
		assert.NilError(t, err, "got: %s", out)
		return out
	}

	installArgs := []string{
		"--python-version", spec,
		"--install-dir", s.InstallDir,
		"--cache-nonce", s.CacheKeyNonce,
	}
	previousPyenv := pyenvGlobal(t)
	out := run(t, "", installArgs...)
	t.Logf("setup-python: %s", out)
	assert.Assert(t, !strings.Contains(out, "from cache"),
		"a per-run nonce must miss the cache, so this install downloads for real; got: %s", out)

	version := reported(t, out, `Python ([0-9][0-9a-z.]*) installed at`)
	if exactVersion.MatchString(spec) {
		assert.Equal(t, version, spec)
	}
	linked := reported(t, out, `linked into (.+)$`)
	t.Logf("setup-python installed Python %s and linked it into %s", version, linked)

	pyenvName := find(out, `registered Python \S+ with pyenv as (\S+),`)
	if pyenvName != "" {
		restorePyenv(t, filepath.Dir(linked), pyenvName, previousPyenv)
	} else {
		removeLinks(t, linked, version)
	}

	t.Run("ResolvesByNameInALaterStep", func(t *testing.T) {
		for _, name := range []string{"python", "python3"} {
			got, err := sdktest.LaterStep(t, name, "--version")
			assert.NilError(t, err, "a later step could not resolve %s: %s", name, got)
			assert.Equal(t, strings.TrimSpace(got), "Python "+version)
		}
		got, err := sdktest.LaterStep(t, "pip", "--version")
		assert.NilError(t, err, "a later step could not resolve pip: %s", got)
		assert.Assert(t, strings.Contains(got, "(python "+minor(version)+")"), "pip belongs to another Python: %s", got)
	})

	if runtime.GOOS == "windows" {
		t.Run("ResolvesInPowerShellToo", func(t *testing.T) {
			got, err := sdktest.LaterStep(t, "powershell.exe", "-Command", "python --version; pip --version")
			assert.NilError(t, err, "PowerShell could not resolve python and pip: %s", got)
			assert.Assert(t, strings.Contains(got, "Python "+version), "PowerShell resolved a different python: %s", got)
		})
	}

	t.Run("PipInstallsResolveInALaterStepWhereTheyCan", func(t *testing.T) {
		got, err := sdktest.LaterStep(t, "pip", "install", "--disable-pip-version-check", "pyflakes==3.2.0")
		assert.NilError(t, err, "pip install: %s", got)
		if strings.Contains(out, "which is not on PATH, so run them with python -m") {
			t.Logf("pip's user scripts directory is not on PATH on this executor; checking python -m instead")
			got, err = sdktest.LaterStep(t, "python", "-m", "pyflakes", "--version")
		} else {
			assert.Assert(t, strings.Contains(out, "pip installs packages for the user") || pyenvName != "", "got: %s", out)
			got, err = sdktest.LaterStep(t, "pyflakes", "--version")
		}
		assert.NilError(t, err, "a later step could not run what pip install installed: %s", got)
		assert.Assert(t, strings.HasPrefix(strings.TrimSpace(got), "3.2.0"), "pyflakes --version: %s", got)
	})

	t.Run("InstallsPackagesWithTheInstalledPip", func(t *testing.T) {
		app := t.TempDir()
		requirements := "# smoke-" + strings.ToLower(s.CacheKeyNonce) + "\nsix==1.17.0\n"
		assert.NilError(t, os.WriteFile(filepath.Join(app, "requirements.txt"), []byte(requirements), 0600))

		out := run(t, app, "install-packages", "--package-manager", "pip")
		t.Logf("install-packages: %s", out)
		got, err := sdktest.LaterStep(t, "python", "-c", "import six; print(six.__version__)")
		assert.NilError(t, err, "the installed package does not import: %s", got)
		assert.Equal(t, strings.TrimSpace(got), "1.17.0")
	})

	t.Run("RestoresFromTheCacheOnAFreshExecutor", func(t *testing.T) {
		skip.If(t, !sdk.InCircleCIJob(), "the cache round trip needs a CircleCI job's cache")
		assert.NilError(t, os.RemoveAll(s.InstallDir))
		out := run(t, "", installArgs...)
		assert.Assert(t, strings.Contains(out, "restored Python "+version+" from cache"),
			"expected the install saved above to be restored; got: %s", out)
		got, err := sdktest.LaterStep(t, "python", "--version")
		assert.NilError(t, err, got)
		assert.Equal(t, strings.TrimSpace(got), "Python "+version)
	})

	t.Run("NothingIsExportedToAShell", func(t *testing.T) {
		content, err := os.ReadFile(bashEnv) //nolint:gosec // a path this test chose
		if os.IsNotExist(err) {
			return
		}
		assert.NilError(t, err)
		assert.Assert(t, !strings.Contains(string(content), "export PATH="),
			"python goes on PATH by being linked there, so nothing should be exported: %s", content)
	})

	if pyenvName != "" {
		t.Run("PyenvRunsItAfterARehash", func(t *testing.T) {
			got, err := sdktest.LaterStep(t, pyenv(), "rehash")
			assert.NilError(t, err, "pyenv rehash: %s", got)
			got, err = sdktest.LaterStep(t, pyenv(), "which", "python")
			assert.NilError(t, err, "pyenv which python: %s", got)
			assert.Assert(t, strings.HasPrefix(realPath(strings.TrimSpace(got)), realPath(s.InstallDir)+string(os.PathSeparator)),
				"pyenv runs %s, not the install under %s", got, s.InstallDir)
			got, err = sdktest.LaterStep(t, "python", "--version")
			assert.NilError(t, err, got)
			assert.Equal(t, strings.TrimSpace(got), "Python "+version)
		})
		return
	}

	t.Run("NotIntoADirectoryOwnedByAnotherTool", func(t *testing.T) {
		for _, part := range strings.Split(filepath.ToSlash(linked), "/") {
			assert.Assert(t, part != "shims",
				"a version manager rewrites its shims directory, taking the link with it: %s", linked)
		}
	})
}

// pyenv returns the pyenv on PATH or, when only its shims are, the one in the
// root they belong to, or "" with no pyenv at all.
func pyenv() string {
	if bin, err := exec.LookPath("pyenv"); err == nil {
		return bin
	}
	python, err := exec.LookPath("python3")
	if err != nil || filepath.Base(filepath.Dir(python)) != "shims" {
		return ""
	}
	root := filepath.Dir(filepath.Dir(python))
	for _, bin := range []string{filepath.Join(root, "bin", "pyenv"), filepath.Join(root, "libexec", "pyenv")} {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}
	return ""
}

// pyenvGlobal returns pyenv's global version, or "" with no pyenv.
func pyenvGlobal(t *testing.T) string {
	t.Helper()
	bin := pyenv()
	if bin == "" {
		return ""
	}
	got, err := sdktest.LaterStep(t, bin, "global")
	assert.NilError(t, err, "pyenv global: %s", got)
	return strings.Join(strings.Fields(got), " ")
}

// restorePyenv unregisters the install from the pyenv at root at the end of
// the test, and puts pyenv's global version back.
func restorePyenv(t *testing.T, root, name, previous string) {
	t.Helper()
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(root, "versions", name))
		bin := pyenv()
		if previous != "" {
			_, _ = sdktest.LaterStep(t, bin, append([]string{"global"}, strings.Fields(previous)...)...)
		}
		_, _ = sdktest.LaterStep(t, bin, "rehash")
	})
}

// minor returns the "3.X" of a version such as "3.13.1".
func minor(version string) string {
	parts := strings.SplitN(version, ".", 3)
	return parts[0] + "." + parts[1]
}

// removeLinks takes python, pip and the package the test installed back off
// PATH at the end of the test.
func removeLinks(t *testing.T, dir, version string) {
	t.Helper()
	t.Cleanup(func() {
		names := []string{"python", "python3", "python" + minor(version), "pip", "pip3", "pip" + minor(version), "pyflakes"}
		for _, name := range names {
			for _, ext := range []string{"", ".exe", ".bat", ".cmd"} {
				_ = os.Remove(filepath.Join(dir, name+ext))
			}
		}
	})
}

// reported returns the first submatch of pattern in setup-python's output.
func reported(t *testing.T, out, pattern string) string {
	t.Helper()
	if got := find(out, pattern); got != "" {
		return got
	}
	t.Fatalf("setup-python did not report %q:\n%s", pattern, out)
	return ""
}

// find returns the first submatch of pattern in out, or "".
func find(out, pattern string) string {
	re := regexp.MustCompile(pattern)
	for line := range strings.SplitSeq(out, "\n") {
		if m := re.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return m[1]
		}
	}
	return ""
}

// realPath resolves symlinks, so a macOS /var answer compares equal to the
// /private/var it points at.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
