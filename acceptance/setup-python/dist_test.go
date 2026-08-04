package setuppython_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

// resolvePipBin mirrors python.ResolveBinaries' preference order (prefer
// "python"/"pip" on PATH, else "python3"/"pip3"), so this black-box test can
// check whether the pip that `dist` will actually invoke already has
// wheel/build installed, without importing the CLI's internal package.
func resolvePipBin() string {
	if _, err := exec.LookPath("python"); err == nil {
		return "pip"
	}
	return "pip3"
}

// requirePipPackage skips the test unless pkg is already importable via the
// pip that `dist` will resolve to, under the exact $HOME the subprocess will
// run with. `dist` always runs `pip install wheel` (or `pip install build`)
// before building; when the package is already satisfied there, pip confirms
// that instantly with no network access, but a cold install would need to
// reach PyPI -- which this suite must not depend on (see this package's
// "must pass hermetically" requirement). Checking against `home` rather than
// this test process's own ambient $HOME matters because `pip install --user`
// packages live under $HOME: a package installed for the real, ambient home
// (e.g. by a developer's own machine setup) would otherwise look
// "available" here but be invisible to the subprocess once $HOME is
// overridden to an empty per-test directory.
func requirePipPackage(t *testing.T, home, pkg string) {
	t.Helper()
	bin := resolvePipBin()
	if _, err := exec.LookPath(bin); err != nil {
		t.Skipf("%s not found on PATH", bin)
	}
	//nolint:gosec // bin/pkg are fixed, known values
	cmd := exec.Command(bin, "show", pkg)
	cmd.Env = append(os.Environ(), "HOME="+home)
	if err := cmd.Run(); err != nil {
		t.Skipf("%s not already installed for %s; dist would need network to fetch it", pkg, bin)
	}
}

// TestCLI_Dist covers both build-tool values, asserting the built
// distribution(s) land in --out-dir. The fixture project
// (testdata/dist-fixture) has no dependencies of its own, so building it
// needs no network; the only network-shaped step `dist` performs is
// installing its own build tool (wheel/build), guarded by requirePipPackage
// above.
//
// The fixture directory is named "dist-fixture" rather than "dist" because
// the repo's top-level .gitignore has an unanchored `dist/` rule (for the
// `task dry-run`/`task ci` build-output dir) that would otherwise also match
// -- and silently exclude from `git add` -- a testdata/dist directory here.
func TestCLI_Dist(t *testing.T) {
	tests := []struct {
		name      string
		buildTool string
		needsPkg  string
		extraArgs []string
		wantFiles []string
	}{
		{
			name:      "wheel",
			buildTool: "wheel",
			needsPkg:  "wheel",
			// setup.py's own "sdist" step ignores --dist-dir and always
			// writes to <app-dir>/dist; only the wheel from "bdist_wheel"
			// respects --dist-dir, so --out-dir only ever gets the wheel.
			wantFiles: []string{"dist_fixture-0.1.0-py3-none-any.whl"},
		},
		{
			name:      "build",
			buildTool: "build",
			needsPkg:  "build",
			// --no-isolation: without it, `python -m build` creates an
			// isolated build env and fetches its own build dependencies
			// from PyPI even though they're already available locally.
			extraArgs: []string{"--no-isolation"},
			wantFiles: []string{"dist_fixture-0.1.0-py3-none-any.whl", "dist_fixture-0.1.0.tar.gz"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			requirePipPackage(t, home, tc.needsPkg)

			appDir := t.TempDir()
			copyFixture(t, "testdata/dist-fixture", appDir)
			outDir := filepath.Join(t.TempDir(), "dist-out")

			env := map[string]string{
				"HOME":                          home,
				"BASH_ENV":                      filepath.Join(t.TempDir(), "bash_env"),
				"PIP_DISABLE_PIP_VERSION_CHECK": "1",
			}

			args := []string{"dist", "--app-dir", appDir, "--build-tool", tc.buildTool, "--out-dir", outDir}
			args = append(args, tc.extraArgs...)

			out, err := run(t, env, args...)
			assert.NilError(t, err, "output: %s", out)

			for _, want := range tc.wantFiles {
				_, statErr := os.Stat(filepath.Join(outDir, want))
				assert.NilError(t, statErr, "expected %s to exist in %s; output: %s", want, outDir, out)
			}
		})
	}
}
