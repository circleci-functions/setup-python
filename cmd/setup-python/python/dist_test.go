package python_test

import (
	"context"
	"errors"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/functions/cmd/setup-python/python"
)

var testBinaries = python.Binaries{Python: "python", Pip: "pip"}

// TestDistCommands_Wheel verifies build-tool=wheel produces "pip install
// wheel" followed by "python setup.py sdist bdist_wheel --dist-dir
// <out-dir>", per porting.md's dist command table.
func TestDistCommands_Wheel(t *testing.T) {
	commands, err := python.DistCommands(testBinaries, python.DistOptions{
		BuildTool: "wheel",
		OutDir:    "./dist",
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, commands, []python.DistCommand{
		{Bin: "pip", Args: []string{"install", "wheel"}},
		{Bin: "python", Args: []string{"setup.py", "sdist", "bdist_wheel", "--dist-dir", "./dist"}},
	})
}

// TestDistCommands_Build_NoExtraFlags verifies build-tool=build with every
// bool flag left false produces just "pip install build" and
// "python -m build --outdir <out-dir> .", with none of the optional flags.
func TestDistCommands_Build_NoExtraFlags(t *testing.T) {
	commands, err := python.DistCommands(testBinaries, python.DistOptions{
		BuildTool: "build",
		OutDir:    "./dist",
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, commands, []python.DistCommand{
		{Bin: "pip", Args: []string{"install", "build"}},
		{Bin: "python", Args: []string{"-m", "build", "--outdir", "./dist", "."}},
	})
}

// TestDistCommands_Build_AllFlags verifies build-tool=build with every bool
// flag set to true adds --sdist, --wheel, --skip-dependency-check, and
// --no-isolation (in that order) before the trailing "." positional arg,
// alongside --outdir.
func TestDistCommands_Build_AllFlags(t *testing.T) {
	commands, err := python.DistCommands(testBinaries, python.DistOptions{
		BuildTool:           "build",
		OutDir:              "./out",
		SdistSeparate:       true,
		WheelSeparate:       true,
		SkipDependencyCheck: true,
		NoIsolation:         true,
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, commands, []python.DistCommand{
		{Bin: "pip", Args: []string{"install", "build"}},
		{Bin: "python", Args: []string{
			"-m", "build", "--outdir", "./out",
			"--sdist", "--wheel", "--skip-dependency-check", "--no-isolation", ".",
		}},
	})
}

// TestDistCommands_UnknownBuildTool verifies an unrecognized build tool
// errors rather than silently returning no commands.
func TestDistCommands_UnknownBuildTool(t *testing.T) {
	_, err := python.DistCommands(testBinaries, python.DistOptions{BuildTool: "bogus"})
	assert.ErrorContains(t, err, "bogus")
}

// TestDistRunDist verifies RunDist invokes the runner once per DistCommand,
// in order, with opts.AppDir as the working directory, and stops at the
// first error.
func TestDistRunDist(t *testing.T) {
	type call struct {
		Bin  string
		Args []string
		Dir  string
	}
	var calls []call

	run := func(_ context.Context, bin string, args []string, dir string) error {
		calls = append(calls, call{Bin: bin, Args: args, Dir: dir})
		return nil
	}

	err := python.RunDist(context.Background(), run, testBinaries, python.DistOptions{
		AppDir:    "/app",
		BuildTool: "wheel",
		OutDir:    "./dist",
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, calls, []call{
		{Bin: "pip", Args: []string{"install", "wheel"}, Dir: "/app"},
		{Bin: "python", Args: []string{"setup.py", "sdist", "bdist_wheel", "--dist-dir", "./dist"}, Dir: "/app"},
	})
}

// TestDistRunDist_StopsAtFirstError verifies RunDist doesn't invoke the
// second command when the first one fails.
func TestDistRunDist_StopsAtFirstError(t *testing.T) {
	wantErr := errors.New("boom")
	calls := 0
	runFailing := func(context.Context, string, []string, string) error {
		calls++
		return wantErr
	}

	err := python.RunDist(context.Background(), runFailing, testBinaries, python.DistOptions{
		BuildTool: "wheel",
		OutDir:    "./dist",
	})
	assert.ErrorIs(t, err, wantErr)
	assert.Equal(t, calls, 1, "expected RunDist to stop after the first failing command")
}
