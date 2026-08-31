package clitest_test

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// TestCLI_Dist checks the subprocess sequence the CLI runs for each build tool,
// observed through the recording stub -- the build-tool install followed by the
// build invocation, in order -- without a real python or build backend.
func TestCLI_Dist(t *testing.T) {
	tests := []struct {
		name      string
		buildTool string
		extraArgs []string
		wantCalls []string
	}{
		{
			name:      "wheel",
			buildTool: "wheel",
			wantCalls: []string{
				"pip install wheel",
				"python setup.py sdist bdist_wheel --dist-dir OUTDIR",
			},
		},
		{
			name:      "build",
			buildTool: "build",
			extraArgs: []string{"--sdist-separate", "--no-isolation"},
			wantCalls: []string{
				"pip install build",
				"python -m build --outdir OUTDIR --sdist --no-isolation .",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, run := sandbox(t)
			env, logPath := stubTools(t, pythonStubs...)
			appDir := copyFixture(t, "distproject")
			outDir := t.TempDir()

			args := append([]string{"dist", "--app-dir", appDir, "--build-tool", tc.buildTool, "--out-dir", outDir}, tc.extraArgs...)
			out, err := run(t, env, args...)
			assert.NilError(t, err, "output: %s", out)

			calls := stubCalls(t, logPath)
			assert.Equal(t, len(calls), len(tc.wantCalls), "calls: %v", calls)
			for i, want := range tc.wantCalls {
				assert.Equal(t, calls[i], strings.ReplaceAll(want, "OUTDIR", outDir))
			}
		})
	}
}
