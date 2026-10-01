// Command setup-python installs a project's dependencies, through its
// install-packages subcommand, with the detected (or explicitly selected)
// package manager, restoring/saving its venv and pypi cache directories via
// the CircleCI job cache around the install.
//
// Supported platforms: linux, darwin, and windows.
package main

import (
	"context"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"
)

type cli struct {
	InstallPackages installPackagesCmd `cmd:"" default:"withargs" help:"Install the project's dependencies with its package manager, restoring/saving its venv and pypi caches around the install."`
}

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-python"),
		kong.Description("Install Python packages using the detected or specified package manager, restoring/saving its venv and pypi caches around the install.\n\nExample:\n\n    circleci run setup-python@<version> -- install-packages --app-dir ./service"),
		kong.UsageOnError(),
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	return kCtx.Run(ctx)
}
