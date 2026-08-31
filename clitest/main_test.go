// Package clitest_test exercises the setup-python CLI contract end to end
// against fakes: a fake CircleCI plugin server (sdktest's sandbox) and no-op
// tool stubs on PATH. It is hermetic -- no real package manager and no network
// -- because every test drives the compiled binary through a curated PATH whose
// only python/pip/poetry/pipenv/uv are stubs that exit 0 without installing
// anything. Real installs and real-infra cache round-trips are covered by
// acceptance/.
package clitest_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

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

// sandbox returns a sandbox plus a run function bound to its env. It is the
// only way to run the binary in this package, so no test can install into the
// developer's real home directory or reach a cache it did not set up. The cache
// is always the fresh in-memory one -- clitest never touches CircleCI's cache.
func sandbox(t *testing.T) (*sdktest.Sandbox, runFunc) {
	t.Helper()
	s := sdktest.NewSandbox(t)
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

// stubTools writes a no-op, argv-recording stub for each named tool into a
// fresh dir and returns the env a run should use: PATH pointing only at that
// dir (so the real python/pip/poetry/uv on the host are never reached -- the
// suite is hermetic on any machine), and STUB_LOG naming a file each stub
// appends its invocation to. Pass the returned map as extraEnv.
//
// The stubs use only POSIX shell builtins (no basename/echo-from-PATH) because
// PATH holds nothing but the stubs themselves. ${0##*/} recovers the tool name
// exec resolved.
func stubTools(t *testing.T, tools ...string) (env map[string]string, logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(t.TempDir(), "stub.log")
	script := "#!/bin/sh\nprintf '%s %s\\n' \"${0##*/}\" \"$*\" >> \"$STUB_LOG\"\nexit 0\n"
	for _, name := range tools {
		if name == "" { // callers append an optional per-manager tool that may be empty
			continue
		}
		//nolint:gosec // test stub with fixed content; 0700 so it is executable
		assert.NilError(t, os.WriteFile(filepath.Join(dir, name), []byte(script), 0700))
	}
	return map[string]string{"PATH": dir, "STUB_LOG": logPath}, logPath
}

// pythonStubs is the set every install/dist run needs on PATH: the CLI resolves
// (and, when computing a python-aware cache key, runs) python/pip before it
// ever looks at the selected package manager.
var pythonStubs = []string{"python", "python3", "pip", "pip3"}

// stubCalls returns the invocations recorded in the stub log, one line per
// exec, each "<tool> <args>". A missing log means nothing was invoked.
func stubCalls(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath) //nolint:gosec // logPath is a test temp file
	if os.IsNotExist(err) {
		return nil
	}
	assert.NilError(t, err)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
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
