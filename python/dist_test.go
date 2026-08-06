package python_test

import (
	"context"
	"errors"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-python/python"
)

var testBinaries = python.Binaries{Python: "python", Pip: "pip"}

// TestDistCommands verifies the exact commands, in order, that each build tool
// produces: the pip install of the build tool itself followed by the build
// invocation, with the optional build flags appended in a fixed order before
// the trailing "." positional argument.
func TestDistCommands(t *testing.T) {
	tests := []struct {
		name    string
		opts    python.DistOptions
		want    []python.DistCommand
		wantErr string
	}{
		{
			name: "wheel",
			opts: python.DistOptions{BuildTool: "wheel", OutDir: "./dist"},
			want: []python.DistCommand{
				{Bin: "pip", Args: []string{"install", "wheel"}},
				{Bin: "python", Args: []string{"setup.py", "sdist", "bdist_wheel", "--dist-dir", "./dist"}},
			},
		},
		{
			name: "build with no optional flags",
			opts: python.DistOptions{BuildTool: "build", OutDir: "./dist"},
			want: []python.DistCommand{
				{Bin: "pip", Args: []string{"install", "build"}},
				{Bin: "python", Args: []string{"-m", "build", "--outdir", "./dist", "."}},
			},
		},
		{
			name: "build with sdist separate",
			opts: python.DistOptions{BuildTool: "build", OutDir: "./dist", SdistSeparate: true},
			want: []python.DistCommand{
				{Bin: "pip", Args: []string{"install", "build"}},
				{Bin: "python", Args: []string{"-m", "build", "--outdir", "./dist", "--sdist", "."}},
			},
		},
		{
			name: "build with wheel separate",
			opts: python.DistOptions{BuildTool: "build", OutDir: "./dist", WheelSeparate: true},
			want: []python.DistCommand{
				{Bin: "pip", Args: []string{"install", "build"}},
				{Bin: "python", Args: []string{"-m", "build", "--outdir", "./dist", "--wheel", "."}},
			},
		},
		{
			name: "build with skip dependency check",
			opts: python.DistOptions{BuildTool: "build", OutDir: "./dist", SkipDependencyCheck: true},
			want: []python.DistCommand{
				{Bin: "pip", Args: []string{"install", "build"}},
				{Bin: "python", Args: []string{"-m", "build", "--outdir", "./dist", "--skip-dependency-check", "."}},
			},
		},
		{
			name: "build with no isolation",
			opts: python.DistOptions{BuildTool: "build", OutDir: "./dist", NoIsolation: true},
			want: []python.DistCommand{
				{Bin: "pip", Args: []string{"install", "build"}},
				{Bin: "python", Args: []string{"-m", "build", "--outdir", "./dist", "--no-isolation", "."}},
			},
		},
		{
			name: "build with all optional flags",
			opts: python.DistOptions{
				BuildTool:           "build",
				OutDir:              "./out",
				SdistSeparate:       true,
				WheelSeparate:       true,
				SkipDependencyCheck: true,
				NoIsolation:         true,
			},
			want: []python.DistCommand{
				{Bin: "pip", Args: []string{"install", "build"}},
				{Bin: "python", Args: []string{
					"-m", "build", "--outdir", "./out",
					"--sdist", "--wheel", "--skip-dependency-check", "--no-isolation", ".",
				}},
			},
		},
		{
			name:    "unknown build tool errors",
			opts:    python.DistOptions{BuildTool: "bogus", OutDir: "./dist"},
			wantErr: "bogus",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := python.DistCommands(testBinaries, tc.opts)

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				assert.Assert(t, got == nil)
				return
			}
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tc.want)
		})
	}
}

// TestRunDist verifies RunDist invokes the runner once per command, in order,
// with opts.AppDir as the working directory, and stops at the first error.
func TestRunDist(t *testing.T) {
	type call struct {
		Bin  string
		Args []string
		Dir  string
	}

	errBoom := errors.New("boom")

	tests := []struct {
		name      string
		runErr    error
		wantCalls []call
		wantErr   error
	}{
		{
			name: "runs every command in the app dir",
			wantCalls: []call{
				{Bin: "pip", Args: []string{"install", "wheel"}, Dir: "/app"},
				{
					Bin:  "python",
					Args: []string{"setup.py", "sdist", "bdist_wheel", "--dist-dir", "./dist"},
					Dir:  "/app",
				},
			},
		},
		{
			name:   "stops at the first failing command",
			runErr: errBoom,
			wantCalls: []call{
				{Bin: "pip", Args: []string{"install", "wheel"}, Dir: "/app"},
			},
			wantErr: errBoom,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls []call
			run := func(_ context.Context, bin string, args []string, dir string) error {
				calls = append(calls, call{Bin: bin, Args: args, Dir: dir})
				return tc.runErr
			}

			err := python.RunDist(context.Background(), run, testBinaries, python.DistOptions{
				AppDir:    "/app",
				BuildTool: "wheel",
				OutDir:    "./dist",
			})

			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			} else {
				assert.NilError(t, err)
			}
			assert.DeepEqual(t, calls, tc.wantCalls)
		})
	}
}
