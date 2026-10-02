package main

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-python/internal/testing/fakedist"
	"github.com/circleci-functions/setup-python/pythondist"
	"github.com/circleci-functions/setup-python/pythonversion"
)

// fakeLiveList points the live list at a fake server listing releases.
func fakeLiveList(t *testing.T, releases ...fakedist.Release) *fakedist.Server {
	t.Helper()
	srv := fakedist.New(t, releases...)
	old := liveDownloadsURL
	liveDownloadsURL = srv.MetadataURL()
	t.Cleanup(func() { liveDownloadsURL = old })
	return srv
}

func resolveSpec(t *testing.T, raw string) (pythondist.Download, error) {
	t.Helper()
	spec, err := pythonversion.Parse(raw, false, false)
	assert.NilError(t, err)
	platform, err := pythondist.Host()
	assert.NilError(t, err)
	return (&installCmd{}).resolve(context.Background(), spec, platform)
}

func TestResolve_UsesTheBuiltInList(t *testing.T) {
	srv := fakeLiveList(t)

	build, err := resolveSpec(t, "3.13.7")
	assert.NilError(t, err)
	assert.Equal(t, build.Version(), "3.13.7")
	assert.Equal(t, srv.MetadataFetches(), 0, "a release in the built-in list needs no live list")
}

func TestResolve_ChecksTheLiveListForANewerRelease(t *testing.T) {
	srv := fakeLiveList(t, fakedist.Release{Version: "3.99.1"})

	build, err := resolveSpec(t, "3.99")
	assert.NilError(t, err)
	assert.Equal(t, build.Version(), "3.99.1")
	assert.Equal(t, build.URL, srv.Download(fakedist.Release{Version: "3.99.1"}).URL)
	assert.Equal(t, srv.MetadataFetches(), 1)
}

func TestResolve_NoMatchAnywhere(t *testing.T) {
	fakeLiveList(t)

	_, err := resolveSpec(t, "3.99")
	assert.ErrorIs(t, err, pythonversion.ErrNoMatch)
}

func TestResolve_LiveListUnavailable(t *testing.T) {
	srv := fakeLiveList(t)
	liveDownloadsURL = srv.URL + "/missing.json"

	_, err := resolveSpec(t, "3.99")
	assert.ErrorIs(t, err, pythonversion.ErrNoMatch)
	assert.ErrorContains(t, err, "could not check uv's current list")
}
