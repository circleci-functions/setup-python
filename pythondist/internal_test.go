package pythondist

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

// PlatformFor exposes platformFor to the external tests.
var PlatformFor = platformFor

func TestWithin(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "x")
	tests := []struct {
		name string
		ok   bool
	}{
		{name: "python/bin/python3", ok: true},
		{name: "python/", ok: true},
		{name: "../escape", ok: false},
		{name: "python/../../escape", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := within(dest, tc.name)
			assert.Equal(t, err == nil, tc.ok, "%v", err)
		})
	}
}

func TestArchiveName(t *testing.T) {
	got := archiveName("https://github.com/astral-sh/python-build-standalone/releases/download/20260924/cpython-3.13.15%2B20260924-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz")
	assert.Equal(t, got, "cpython-3.13.15+20260924-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz")
}

func TestMuslInterpreter(t *testing.T) {
	assert.Assert(t, muslInterpreter("/lib/ld-musl-x86_64.so.1"))
	assert.Assert(t, muslInterpreter("/lib/ld-musl-aarch64.so.1"))
	assert.Assert(t, !muslInterpreter("/lib64/ld-linux-x86-64.so.2"))
	assert.Assert(t, !muslInterpreter("/lib/ld-linux-aarch64.so.1"))
}

func TestExtract_KeepsModificationTimes(t *testing.T) {
	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	assert.NilError(t, tw.WriteHeader(&tar.Header{Name: "python/lib/os.py", Typeflag: tar.TypeReg, Mode: 0644, Size: 1, ModTime: mtime}))
	_, err := tw.Write([]byte("x"))
	assert.NilError(t, err)
	assert.NilError(t, tw.Close())
	assert.NilError(t, gz.Close())
	src := filepath.Join(t.TempDir(), "a.tar.gz")
	assert.NilError(t, os.WriteFile(src, buf.Bytes(), 0600))

	dest := t.TempDir()
	assert.NilError(t, extractTarGz(src, dest))
	info, err := os.Stat(filepath.Join(dest, "python", "lib", "os.py"))
	assert.NilError(t, err)
	assert.Assert(t, info.ModTime().Equal(mtime), "got %v", info.ModTime())
}
