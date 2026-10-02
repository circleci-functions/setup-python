//go:build acceptance

package setuppython_test

import (
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
)

// laterStepPath returns a PATH holding only the sandbox's ~/.local/bin and the
// system directories, so setup-python links into the sandbox rather than take
// over a Python already installed on the machine, and applies it and the
// sandbox's $HOME to this process for sdktest.LaterStep. It returns the PATH
// entry setup-python should link into, too.
func laterStepPath(t *testing.T, s *sdktest.Sandbox) (map[string]string, string) {
	t.Helper()
	skip.If(t, runtime.GOOS == "windows", "the sandboxed PATH is a Unix one")
	binDir := filepath.Join(s.Home, ".local", "bin")
	assert.NilError(t, os.MkdirAll(binDir, 0750))
	path := strings.Join([]string{binDir, "/usr/bin", "/bin"}, string(os.PathListSeparator))
	t.Setenv("PATH", path)
	t.Setenv("HOME", s.Home)
	return map[string]string{"PATH": path}, binDir
}

func TestAcceptance_InstallPython(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		files map[string]string
		want  string
	}{
		{name: "exact", args: []string{"--python-version", "3.12.11"}, want: "Python 3.12.11"},
		{name: "minor", args: []string{"--python-version", "3.12"}, want: "Python 3.12."},
		{name: "python-version", files: map[string]string{".python-version": "3.13\n"}, want: "Python 3.13."},
		{name: "pyproject.toml", args: []string{"--python-version-file", "pyproject.toml"}, files: map[string]string{"pyproject.toml": "[project]\nrequires-python = \">=3.11,<3.12\"\n"}, want: "Python 3.11."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := sandboxInMemory(t)
			env, binDir := laterStepPath(t, s)
			dir := t.TempDir()
			for name, content := range tc.files {
				assert.NilError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
			}

			r := runner.New(t, binaryPath, runner.WithSandboxEnv(sandboxEnv(s, env)), runner.WithCmdDir(dir))
			_, err := r.Run(tc.args...)
			out := r.StdoutString() + r.StderrString()
			assert.NilError(t, err, "output: %s", out)
			assert.Assert(t, strings.Contains(out, "linked into "+binDir), "output: %s", out)

			got, err := sdktest.LaterStep(t, "python", "--version")
			assert.NilError(t, err, got)
			assert.Assert(t, strings.HasPrefix(strings.TrimSpace(got), tc.want), "python --version: %s", got)

			for _, tool := range []string{"python3", "pip", "pip3"} {
				got, err := sdktest.LaterStep(t, tool, "--version")
				assert.NilError(t, err, "%s: %s", tool, got)
			}
		})
	}
}

func TestAcceptance_InstallPython_Freethreaded(t *testing.T) {
	s, run := sandboxInMemory(t)
	env, _ := laterStepPath(t, s)

	out, err := run(t, env, "--python-version", "3.14t")
	assert.NilError(t, err, "output: %s", out)

	got, err := sdktest.LaterStep(t, "python", "-c", "import sys; print(sys._is_gil_enabled())")
	assert.NilError(t, err, got)
	assert.Equal(t, strings.TrimSpace(got), "False")
}

// TestAcceptance_InstallPython_CacheRoundTrip restores the install from the
// cache on a simulated fresh executor. The cache nonce keeps the cold install
// from restoring an entry an earlier pipeline saved under the same write-once
// key.
func TestAcceptance_InstallPython_CacheRoundTrip(t *testing.T) {
	s, run := sandbox(t)
	env, _ := laterStepPath(t, s)
	args := []string{"--python-version", "3.12.11", "--cache-nonce", s.CacheKeyNonce}

	out, err := run(t, env, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, !strings.Contains(out, "from cache"), "output: %s", out)

	assert.NilError(t, os.RemoveAll(filepath.Join(s.Home, ".cache", "setup-python")))
	out, err = run(t, env, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "restored Python 3.12.11 from cache"), "output: %s", out)

	got, err := sdktest.LaterStep(t, "python", "--version")
	assert.NilError(t, err, got)
	assert.Equal(t, strings.TrimSpace(got), "Python 3.12.11")
}

// TestAcceptance_InstallPython_PipInstallsOnPath checks what pip installs
// resolves by name in a later step, like python itself.
func TestAcceptance_InstallPython_PipInstallsOnPath(t *testing.T) {
	s, run := sandboxInMemory(t)
	env, _ := laterStepPath(t, s)

	out, err := run(t, env, "--python-version", "3.13")
	assert.NilError(t, err, "output: %s", out)

	pip := exec.Command("pip", "install", "--disable-pip-version-check", "pyflakes==3.2.0")
	pip.Env = s.CmdEnv(os.Environ())
	installOut, err := pip.CombinedOutput()
	assert.NilError(t, err, "pip install: %s", installOut)

	got, err := sdktest.LaterStep(t, "pyflakes", "--version")
	assert.NilError(t, err, got)
	assert.Assert(t, strings.HasPrefix(strings.TrimSpace(got), "3.2.0"), "pyflakes --version: %s", got)
}

// TestAcceptance_InstallPython_PipVersion installs a pip older than the one a
// build ships.
func TestAcceptance_InstallPython_PipVersion(t *testing.T) {
	s, run := sandboxInMemory(t)
	env, _ := laterStepPath(t, s)

	out, err := run(t, env, "--python-version", "3.13", "--pip-version", "24.2")
	assert.NilError(t, err, "output: %s", out)

	got, err := sdktest.LaterStep(t, "pip", "--version")
	assert.NilError(t, err, got)
	assert.Assert(t, strings.HasPrefix(got, "pip 24.2 "), "pip --version: %s", got)
}
