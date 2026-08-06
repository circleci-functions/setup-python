package python

import (
	"context"
	"fmt"
)

// Runner executes bin with args in dir, forwarding stdout and stderr to the
// calling process.
type Runner func(ctx context.Context, bin string, args []string, dir string) error

// DistOptions holds the resolved settings for a dist build.
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

// DistCommands returns, in order, the subprocess invocations that build a
// distribution for opts.BuildTool. Nothing is executed:
//
//	wheel -> pip install wheel
//	         python setup.py sdist bdist_wheel --dist-dir <out-dir>
//
//	build -> pip install build
//	         python -m build --outdir <out-dir> [--sdist] [--wheel] [--skip-dependency-check] [--no-isolation] .
//
// Returns an error for an unknown build tool.
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

// RunDist executes the DistCommands for binaries and opts in opts.AppDir,
// stopping at and returning the first error.
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
