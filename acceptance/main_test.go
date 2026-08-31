//go:build acceptance

// Package setuppython_test exercises setup-python against real infrastructure:
// real package-manager installs and dist builds, and a real CircleCI
// task-agent cache when running inside a job. Tagged out of `task test`
// because it needs the tools on PATH and (for a live cache) a job. The CLI
// contract itself is covered hermetically by clitest/.
package setuppython_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/compiler"
	"github.com/circleci-functions/toolkit/testing/runner"
)

var binaryPath string

func TestMain(m *testing.M) {
	status, err := runTests(m)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(status)
}

func runTests(m *testing.M) (int, error) {
	p := compiler.NewParallel(1)
	defer p.Cleanup()

	p.Add(compiler.Work{
		Result: &binaryPath,
		Name:   "setup-python",
		Target: "..",
		Source: ".",
	})

	if err := p.Run(context.Background()); err != nil {
		return 0, err
	}
	return m.Run(), nil
}

// runFunc runs the compiled binary with extraEnv applied on top of the
// sandbox's environment, so entries in extraEnv take precedence.
type runFunc func(t *testing.T, extraEnv map[string]string, args ...string) (string, error)

// sandbox returns a sandbox plus a run function bound to its env.
//
// The cache is CircleCI's own when the caller asks for it with
// TOOLKIT_CACHE_MODE=live -- which the CI job does, so the round-trips here hit
// the real task-agent -- and in-memory otherwise, so the same suite runs on a
// laptop. Either way $HOME/$BASH_ENV/$XDG_CACHE_HOME are temp locations, so an
// install never touches the developer's real ones.
func sandbox(t *testing.T) (*sdktest.Sandbox, runFunc) {
	t.Helper()
	s := sdktest.NewSandbox(t, sdktest.WithLiveCache())
	return s, func(t *testing.T, extraEnv map[string]string, args ...string) (string, error) {
		t.Helper()
		r := runner.New(t, binaryPath, runner.WithSandboxEnv(sandboxEnv(s, extraEnv)))
		_, err := r.Run(args...)
		return r.StdoutString() + r.StderrString(), err
	}
}

// sandboxEnv returns the sandbox's environment with extra applied on top.
func sandboxEnv(s *sdktest.Sandbox, extra map[string]string) map[string]string {
	env := s.Env()
	maps.Copy(env, extra)
	return env
}

// installEnv is the extra env every real install/dist run wants: pip's version
// check off, and virtualenv's periodic PyPI seed-wheel update disabled (it
// otherwise spawns a detached process that can still be writing under $HOME
// after the test exits, racing t.TempDir() cleanup).
func installEnv() map[string]string {
	return map[string]string{
		"PIP_DISABLE_PIP_VERSION_CHECK": "1",
		"VIRTUALENV_NO_PERIODIC_UPDATE": "1",
	}
}

// requireTool skips the test when tool is absent from PATH. CI installs all of
// them; a laptop may have only some, so the suite still runs against whatever
// is present.
func requireTool(t *testing.T, tool string) {
	t.Helper()
	_, err := exec.LookPath(tool)
	skip.If(t, err != nil, tool+" not found on PATH")
}

// copyFixture recursively copies the checked-in fixture named by pm into a
// fresh temp app dir and returns it. It recurses because some fixtures
// (pip-dist, distproject) carry a package subdirectory alongside their
// top-level files.
func copyFixture(t *testing.T, pm string) string {
	t.Helper()
	dst := t.TempDir()
	copyTree(t, fixtureDir(t, pm), dst)
	return dst
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	assert.NilError(t, err)
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			assert.NilError(t, os.MkdirAll(dstPath, 0750))
			copyTree(t, srcPath, dstPath)
			continue
		}
		data, err := os.ReadFile(srcPath) //nolint:gosec // fixture path is controlled by the test suite
		assert.NilError(t, err)
		//nolint:gosec // fixture path is controlled by the test suite; fixtures are not secrets
		assert.NilError(t, os.WriteFile(dstPath, data, 0600))
	}
}

// fixtureDir resolves a checked-in fixture to an absolute path. The binary runs
// with its own temp dir as cwd, so a relative path would resolve against the
// wrong directory.
func fixtureDir(t *testing.T, pm string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "internal", "testing", "testdata", pm))
	if err != nil {
		t.Fatalf("resolve fixture dir: %v", err)
	}
	return abs
}
