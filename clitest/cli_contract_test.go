package clitest_test

import (
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// TestCLI_UnknownFlag rejects an unrecognised flag before doing any work.
func TestCLI_UnknownFlag(t *testing.T) {
	_, run := sandbox(t)
	env, _ := stubTools(t, pythonStubs...)

	out, err := run(t, env, "--nope")
	assert.Assert(t, err != nil, "expected a non-zero exit; output: %s", out)
	assert.Assert(t, strings.Contains(out, "--nope"), "expected the error to name the flag; output: %s", out)
}

// TestCLI_InvalidPackageManager rejects a --package-manager outside the enum.
func TestCLI_InvalidPackageManager(t *testing.T) {
	_, run := sandbox(t)
	env, _ := stubTools(t, pythonStubs...)
	appDir := copyFixture(t, "pip")

	out, err := run(t, env, "install-packages", "--app-dir", appDir, "--package-manager", "conda")
	assert.Assert(t, err != nil, "expected a non-zero exit; output: %s", out)
	assert.Assert(t, strings.Contains(out, "conda"), "expected the error to name the value; output: %s", out)
}

// TestCLI_AppDirMissing fails cleanly when --app-dir does not exist.
func TestCLI_AppDirMissing(t *testing.T) {
	_, run := sandbox(t)
	env, _ := stubTools(t, pythonStubs...)

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	out, err := run(t, env, "install-packages", "--app-dir", missing, "--package-manager", "pip")
	assert.Assert(t, err != nil, "expected a non-zero exit; output: %s", out)
	assert.Assert(t, strings.Contains(out, "app-dir"), "expected an app-dir error; output: %s", out)
}

// TestCLI_ToolNotOnPATH fails with a clear message, not a silent success, when
// the package manager is absent from PATH -- the property that makes the "put a
// stub on PATH" isolation load-bearing. PATH points at an empty dir, so neither
// pip nor python resolves.
func TestCLI_ToolNotOnPATH(t *testing.T) {
	_, run := sandbox(t)
	appDir := copyFixture(t, "pip")

	env := map[string]string{"PATH": t.TempDir()}
	out, err := run(t, env, "install-packages", "--app-dir", appDir, "--package-manager", "pip", "--cache=false")
	assert.Assert(t, err != nil, "expected a non-zero exit; output: %s", out)
	assert.Assert(t, strings.Contains(out, "not found on PATH"), "expected a not-found-on-PATH error; output: %s", out)
}
