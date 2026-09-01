//go:build acceptance

package setuppython_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

// resolvePipBin mirrors python.ResolveBinaries' preference (prefer "pip" when
// "python" is on PATH, else "pip3") so this black-box test can check whether
// the pip that `dist` will invoke already has wheel/build, without importing
// the CLI's internal package.
func resolvePipBin() string {
	if _, err := exec.LookPath("python"); err == nil {
		return "pip"
	}
	return "pip3"
}

// requirePipPackage skips the test unless pkg is importable via the resolved
// pip under the exact $HOME the subprocess runs with. `dist` runs `pip install
// wheel` (or build) first; when already satisfied pip confirms instantly with
// no network, but a cold install would reach PyPI -- which this suite must not
// depend on. Checking against home (the sandbox's) rather than the ambient one
// matters because `pip install --user` packages live under $HOME.
func requirePipPackage(t *testing.T, home, pkg string) {
	t.Helper()
	bin := resolvePipBin()
	_, err := exec.LookPath(bin)
	skip.If(t, err != nil, bin+" not found on PATH")

	//nolint:gosec // bin/pkg are fixed, known values
	cmd := exec.Command(bin, "show", pkg)
	cmd.Env = append(os.Environ(), "HOME="+home)
	skip.If(t, cmd.Run() != nil, pkg+" not already installed for "+bin+"; dist would need network to fetch it")
}

// TestAcceptance_Dist covers both build tools, asserting the built
// distribution(s) land in --out-dir. The project built (testdata/distproject)
// has no dependencies, so building needs no network; the only network-shaped
// step is installing dist's own build tool (wheel/build), guarded above.
func TestAcceptance_Dist(t *testing.T) {
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
			// setup.py's sdist step ignores --dist-dir and writes to
			// <app-dir>/dist; only the bdist_wheel wheel respects --dist-dir.
			wantFiles: []string{"distproject-0.1.0-py3-none-any.whl"},
		},
		{
			name:      "build",
			buildTool: "build",
			needsPkg:  "build",
			// --no-isolation: without it `python -m build` fetches its build
			// deps from PyPI even though they are present locally.
			extraArgs: []string{"--no-isolation"},
			wantFiles: []string{"distproject-0.1.0-py3-none-any.whl", "distproject-0.1.0.tar.gz"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, run := sandbox(t)
			requirePipPackage(t, s.Home, tc.needsPkg)

			appDir := copyFixture(t, "distproject")
			outDir := filepath.Join(t.TempDir(), "dist-out")

			args := append([]string{"dist", "--app-dir", appDir, "--build-tool", tc.buildTool, "--out-dir", outDir}, tc.extraArgs...)
			out, err := run(t, installEnv(), args...)
			assert.NilError(t, err, "output: %s", out)

			for _, want := range tc.wantFiles {
				_, statErr := os.Stat(filepath.Join(outDir, want))
				assert.NilError(t, statErr, "expected %s in %s; output: %s", want, outDir, out)
			}
		})
	}
}
