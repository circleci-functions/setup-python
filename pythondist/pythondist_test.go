package pythondist_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/setup-python/internal/testing/fakedist"
	"github.com/circleci-functions/setup-python/pythondist"
)

var linux = pythondist.Platform{OS: "linux", Arch: "x86_64", Libc: "gnu"}

func TestParse_KeepsOnlyInstallableBuilds(t *testing.T) {
	data := []byte(`{
		"cpython-3.13.1-linux-x86_64-gnu": {"name": "cpython", "arch": {"family": "x86_64", "variant": null}, "os": "linux", "libc": "gnu", "major": 3, "minor": 13, "patch": 1, "prerelease": "", "url": "https://example.com/a.tar.gz", "sha256": "ab", "variant": null},
		"cpython-3.13.1+freethreaded-linux-x86_64-gnu": {"name": "cpython", "arch": {"family": "x86_64", "variant": null}, "os": "linux", "libc": "gnu", "major": 3, "minor": 13, "patch": 1, "prerelease": "", "url": "https://example.com/b.tar.gz", "sha256": "ab", "variant": "freethreaded"},
		"cpython-3.13.1+debug-linux-x86_64-gnu": {"name": "cpython", "arch": {"family": "x86_64", "variant": null}, "os": "linux", "libc": "gnu", "major": 3, "minor": 13, "patch": 1, "prerelease": "", "url": "https://example.com/c.tar.zst", "sha256": "ab", "variant": "debug"},
		"cpython-3.13.1-linux-x86_64_v3-gnu": {"name": "cpython", "arch": {"family": "x86_64", "variant": "v3"}, "os": "linux", "libc": "gnu", "major": 3, "minor": 13, "patch": 1, "prerelease": "", "url": "https://example.com/d.tar.gz", "sha256": "ab", "variant": null},
		"cpython-3.8.0-linux-x86_64-gnu": {"name": "cpython", "arch": {"family": "x86_64", "variant": null}, "os": "linux", "libc": "gnu", "major": 3, "minor": 8, "patch": 0, "prerelease": "", "url": "https://example.com/e.tar.zst", "sha256": "ab", "variant": null},
		"cpython-3.13.1-linux-x86_64-nosum": {"name": "cpython", "arch": {"family": "x86_64", "variant": null}, "os": "linux", "libc": "gnu", "major": 3, "minor": 13, "patch": 1, "prerelease": "", "url": "https://example.com/f.tar.gz", "sha256": null, "variant": null},
		"cpython-3.13.1-emscripten-wasm32-musl": {"name": "cpython", "arch": {"family": "wasm32", "variant": null}, "os": "emscripten", "libc": "musl", "major": 3, "minor": 13, "patch": 1, "prerelease": "", "url": "https://example.com/g.tar.gz", "sha256": "ab", "variant": null},
		"pypy-3.10.14-linux-x86_64-gnu": {"name": "pypy", "arch": {"family": "x86_64", "variant": null}, "os": "linux", "libc": "gnu", "major": 3, "minor": 10, "patch": 14, "prerelease": "", "url": "https://example.com/h.tar.gz", "sha256": "ab", "variant": null}
	}`)
	c, err := pythondist.Parse(data)
	assert.NilError(t, err)
	assert.Equal(t, len(c), 2)
	d, ok := c.For(linux, false).Find("3.13.1")
	assert.Assert(t, ok)
	assert.Equal(t, d.URL, "https://example.com/a.tar.gz")
	d, ok = c.For(linux, true).Find("3.13.1")
	assert.Assert(t, ok)
	assert.Equal(t, d.URL, "https://example.com/b.tar.gz")
}

func TestSnapshot(t *testing.T) {
	c, err := pythondist.Snapshot()
	assert.NilError(t, err)
	for _, p := range []pythondist.Platform{
		linux,
		{OS: "linux", Arch: "aarch64", Libc: "gnu"},
		{OS: "linux", Arch: "x86_64", Libc: "musl"},
		{OS: "linux", Arch: "aarch64", Libc: "musl"},
		{OS: "darwin", Arch: "aarch64", Libc: "none"},
		{OS: "darwin", Arch: "x86_64", Libc: "none"},
		{OS: "windows", Arch: "x86_64", Libc: "none"},
		{OS: "windows", Arch: "aarch64", Libc: "none"},
	} {
		_, ok := c.For(p, false).Find("3.13.7")
		assert.Assert(t, ok, "no 3.13.7 build for %s", p)
	}
	_, ok := c.For(linux, true).Find("3.13.12")
	assert.Assert(t, ok, "no free-threaded 3.13.12 build")
}

func TestCatalog_MarshalRoundTrips(t *testing.T) {
	c, err := pythondist.Snapshot()
	assert.NilError(t, err)
	data, err := c.Marshal()
	assert.NilError(t, err)
	again, err := pythondist.Parse(data)
	assert.NilError(t, err)
	assert.DeepEqual(t, again, c)
}

func TestCatalog_Versions(t *testing.T) {
	srv := fakedist.New(t,
		fakedist.Release{Version: "3.13.1"},
		fakedist.Release{Version: "3.12.4"},
		fakedist.Release{Version: "3.13.1", Freethreaded: true},
		fakedist.Release{Version: "3.14.0", Platform: pythondist.Platform{OS: "plan9", Arch: "x86_64", Libc: "none"}},
	)
	c, err := pythondist.Fetch(context.Background(), srv.MetadataURL())
	assert.NilError(t, err)
	host, err := pythondist.Host()
	assert.NilError(t, err)
	assert.DeepEqual(t, c.For(host, false).Versions(), []string{"3.12.4", "3.13.1"})
	assert.DeepEqual(t, c.For(host, true).Versions(), []string{"3.13.1"})
}

func TestPlatform(t *testing.T) {
	tests := []struct {
		goos, goarch string
		musl         bool
		want         string
	}{
		{goos: "linux", goarch: "amd64", want: "linux-x86_64-gnu"},
		{goos: "linux", goarch: "arm64", want: "linux-aarch64-gnu"},
		{goos: "linux", goarch: "amd64", musl: true, want: "linux-x86_64-musl"},
		{goos: "linux", goarch: "arm64", musl: true, want: "linux-aarch64-musl"},
		{goos: "linux", goarch: "arm", want: "linux-armv7-gnueabihf"},
		{goos: "linux", goarch: "ppc64le", want: "linux-powerpc64le-gnu"},
		{goos: "linux", goarch: "s390x", want: "linux-s390x-gnu"},
		{goos: "linux", goarch: "riscv64", want: "linux-riscv64-gnu"},
		{goos: "linux", goarch: "386", want: "linux-i686-gnu"},
		{goos: "darwin", goarch: "arm64", want: "darwin-aarch64"},
		{goos: "darwin", goarch: "amd64", want: "darwin-x86_64"},
		{goos: "windows", goarch: "amd64", want: "windows-x86_64"},
		{goos: "windows", goarch: "arm64", want: "windows-aarch64"},
		{goos: "windows", goarch: "386", want: "windows-i686"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			p, err := pythondist.PlatformFor(tc.goos, tc.goarch, tc.musl)
			assert.NilError(t, err)
			assert.Equal(t, p.String(), tc.want)
		})
	}
}

func TestPlatform_Unsupported(t *testing.T) {
	_, err := pythondist.PlatformFor("freebsd", "amd64", false)
	assert.ErrorContains(t, err, "unsupported operating system freebsd")
	_, err = pythondist.PlatformFor("linux", "mips", false)
	assert.ErrorContains(t, err, "unsupported architecture mips")
}

func TestInstall(t *testing.T) {
	tests := []struct {
		name     string
		platform pythondist.Platform
		want     []string
	}{
		{name: "unix", platform: linux, want: []string{"bin/python3.13", "bin/python", "bin/python3", "bin/pip", "bin/pip3", "bin/pip3.13"}},
		{name: "windows", platform: pythondist.Platform{OS: "windows", Arch: "x86_64", Libc: "none"}, want: []string{"python.exe", "python313.dll", "Scripts", "Lib/site-packages/pip"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			skip.If(t, runtime.GOOS == "windows" && tc.platform.OS != "windows", "the Unix archive holds symlinks")
			rel := fakedist.Release{Version: "3.13.1", Platform: tc.platform}
			srv := fakedist.New(t, rel)
			dest := filepath.Join(t.TempDir(), "python")

			assert.NilError(t, pythondist.Install(context.Background(), srv.Download(rel), dest))
			for _, f := range tc.want {
				_, err := os.Lstat(filepath.Join(dest, filepath.FromSlash(f)))
				assert.NilError(t, err, f)
			}
			entries, err := os.ReadDir(filepath.Dir(dest))
			assert.NilError(t, err)
			assert.Equal(t, len(entries), 1, "download and staging leftovers: %v", entries)
		})
	}
}

func TestInstall_UnixLayoutRuns(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "the fake build's python is a shell script")
	rel := fakedist.Release{Version: "3.13.1"}
	srv := fakedist.New(t, rel)
	dest := filepath.Join(t.TempDir(), "python")

	assert.NilError(t, pythondist.Install(context.Background(), srv.Download(rel), dest))

	info, err := os.Stat(filepath.Join(dest, "bin", "python3"))
	assert.NilError(t, err)
	assert.Assert(t, info.Mode().Perm()&0100 != 0, "bin/python3 is not executable: %v", info.Mode())
	target, err := os.Readlink(filepath.Join(dest, "bin", "python3"))
	assert.NilError(t, err)
	assert.Equal(t, target, "python3.13")
}

func TestInstall_ReplacesIncompleteInstall(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "the Unix archive holds symlinks")
	rel := fakedist.Release{Version: "3.13.1"}
	srv := fakedist.New(t, rel)
	dest := filepath.Join(t.TempDir(), "python")
	assert.NilError(t, os.MkdirAll(filepath.Join(dest, "lib"), 0750))
	assert.NilError(t, os.WriteFile(filepath.Join(dest, "lib", "leftover"), []byte("x"), 0600))

	assert.NilError(t, pythondist.Install(context.Background(), srv.Download(rel), dest))

	_, err := os.Stat(filepath.Join(dest, "bin", "python3"))
	assert.NilError(t, err)
	_, err = os.Stat(filepath.Join(dest, "lib", "leftover"))
	assert.Assert(t, os.IsNotExist(err), "the incomplete install was not replaced")
}

func TestInstall_Errors(t *testing.T) {
	rel := fakedist.Release{Version: "3.13.1"}
	tests := []struct {
		name    string
		corrupt bool
		modify  func(d *pythondist.Download)
		wantErr string
	}{
		{name: "checksum mismatch", corrupt: true, wantErr: "checksum mismatch"},
		{name: "missing archive", modify: func(d *pythondist.Download) { d.URL += ".missing" }, wantErr: "download "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakedist.New(t, rel)
			if tc.corrupt {
				srv.Corrupt()
			}
			d := srv.Download(rel)
			if tc.modify != nil {
				tc.modify(&d)
			}
			dest := filepath.Join(t.TempDir(), "python")

			err := pythondist.Install(context.Background(), d, dest)
			assert.ErrorContains(t, err, tc.wantErr)
			_, statErr := os.Stat(dest)
			assert.Assert(t, os.IsNotExist(statErr), "a failed install left %s behind", dest)
		})
	}
}
