// Command update-downloads refreshes the catalog of Python builds embedded in
// setup-python from uv's current download metadata, keeping only the builds
// setup-python can install.
//
//	go run ./internal/cmd/update-downloads [-o pythondist/downloads.json]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/circleci-functions/setup-python/pythondist"
)

func main() {
	out := flag.String("o", "pythondist/downloads.json", "file to write the catalog to")
	url := flag.String("url", pythondist.DefaultURL, "uv download metadata to read")
	flag.Parse()

	if err := run(context.Background(), *url, *out); err != nil {
		fmt.Fprintln(os.Stderr, "update-downloads:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, url, out string) error {
	catalog, err := pythondist.Fetch(ctx, url)
	if err != nil {
		return err
	}
	data, err := catalog.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, data, 0644); err != nil { //nolint:gosec // a checked-in source file
		return err
	}
	fmt.Printf("wrote %d builds to %s\n", len(catalog), out)
	return nil
}
