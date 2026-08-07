// Package setuppython_test contains acceptance tests for setup-python: it
// compiles the real binary in TestMain (mirroring acceptance/setup-node) and
// runs it as a subprocess via os/exec, verifying the actual CLI contract
// rather than calling Go functions directly.
package setuppython_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/circleci-functions/toolkit/testing/compiler"
	"github.com/circleci-functions/toolkit/testing/runner"
	"gotest.tools/v3/assert"
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
	ctx := context.Background()

	p := compiler.NewParallel(1)
	defer p.Cleanup()

	p.Add(compiler.Work{
		Result: &binaryPath,
		Name:   "setup-python",
		Target: "../..",
		Source: ".",
	})

	if err := p.Run(ctx); err != nil {
		return 0, err
	}

	return m.Run(), nil
}

// run runs the compiled binary, with extraEnv merged on top of the inherited
// environment. Pass a nil extraEnv when no overrides are needed.
func run(t *testing.T, extraEnv map[string]string, args ...string) (string, error) {
	t.Helper()
	r := runner.New(t, binaryPath, runner.WithUnsafeOSEnvVars(), runner.WithUnsafeAdditionalEnvVars(extraEnv))
	_, err := r.Run(args...)
	return r.StdoutString() + r.StderrString(), err
}

// copyFixture recursively copies the contents of src into dst, used to seed
// a fresh --app-dir per test run from a testdata fixture without mutating
// the fixture itself. Unlike acceptance/setup-node's flat copyFixture, this
// one recurses since some setup-python fixtures (pip-dist, distproject)
// include a local package subdirectory alongside their top-level files.
func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	assert.NilError(t, err)
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			assert.NilError(t, os.MkdirAll(dstPath, 0750))
			copyFixture(t, srcPath, dstPath)
			continue
		}
		data, err := os.ReadFile(srcPath) //nolint:gosec // test fixture path
		assert.NilError(t, err)
		// #nosec G703: Test fixture paths are controlled within the test suite
		assert.NilError(t, os.WriteFile(dstPath, data, 0600))
	}
}
