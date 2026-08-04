package python

import (
	"context"
	"fmt"
)

// Runner executes bin with args in dir, forwarding stdout/stderr to the
// calling process's own. It matches the shape of the runInstall/runBuild
// exec seams in cmd/setup-python/main.go, letting RunDist (and its callers'
// tests) fake command execution without a real pip/python on PATH.
type Runner func(ctx context.Context, bin string, args []string, dir string) error

// DistOptions holds the resolved settings for a `dist` build, mirroring
// distCmd's flags in main.go (see porting.md's dist flag-mapping table).
type DistOptions struct {
	AppDir              string
	BuildTool           string
	OutDir              string
	NoIsolation         bool
	SdistSeparate       bool
	SkipDependencyCheck bool
	WheelSeparate       bool
}

// DistCommand is a single subprocess invocation making up a dist build.
type DistCommand struct {
	Bin  string
	Args []string
}

// DistCommands returns, in order, the subprocess invocations required to
// build a distribution for opts.BuildTool, per porting.md's dist command
// table:
//
//	wheel -> pip install wheel
//	         python setup.py sdist bdist_wheel --dist-dir <out-dir>
//
//	build -> pip install build
//	         python -m build --outdir <out-dir> [--sdist] [--wheel] [--skip-dependency-check] [--no-isolation] .
//
// Pure and side-effect free (no subprocess is run), so callers/tests can
// assert on the exact argv without executing anything.
func DistCommands(binaries Binaries, opts DistOptions) ([]DistCommand, error) {
	switch opts.BuildTool {
	case "wheel":
		return []DistCommand{
			{Bin: binaries.Pip, Args: []string{"install", "wheel"}},
			{Bin: binaries.Python, Args: []string{"setup.py", "sdist", "bdist_wheel", "--dist-dir", opts.OutDir}},
		}, nil
	case "build":
		args := []string{"-m", "build", "--outdir", opts.OutDir}
		if opts.SdistSeparate {
			args = append(args, "--sdist")
		}
		if opts.WheelSeparate {
			args = append(args, "--wheel")
		}
		if opts.SkipDependencyCheck {
			args = append(args, "--skip-dependency-check")
		}
		if opts.NoIsolation {
			args = append(args, "--no-isolation")
		}
		args = append(args, ".")
		return []DistCommand{
			{Bin: binaries.Pip, Args: []string{"install", "build"}},
			{Bin: binaries.Python, Args: args},
		}, nil
	default:
		return nil, fmt.Errorf("unknown build tool %q", opts.BuildTool)
	}
}

// RunDist executes, in order, the DistCommands for binaries/opts via run in
// opts.AppDir, stopping at (and returning) the first error.
func RunDist(ctx context.Context, run Runner, binaries Binaries, opts DistOptions) error {
	commands, err := DistCommands(binaries, opts)
	if err != nil {
		return err
	}
	for _, cmd := range commands {
		if err := run(ctx, cmd.Bin, cmd.Args, opts.AppDir); err != nil {
			return err
		}
	}
	return nil
}
