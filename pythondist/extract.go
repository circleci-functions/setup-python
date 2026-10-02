package pythondist

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/circleci-functions/toolkit/archive"
)

func extractTarGz(src, dest string) error {
	f, err := os.Open(src) //nolint:gosec // src is the archive this package just downloaded
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		if err := extractTarEntry(tr, hdr, dest); err != nil {
			return err
		}
	}
}

// extractTarEntry writes one entry under dest. Regular files keep their
// modification time: the bytecode shipped beside each module records its
// source's, and Python recompiles a module whose times disagree.
func extractTarEntry(tr *tar.Reader, hdr *tar.Header, dest string) error {
	target, err := within(dest, hdr.Name)
	if err != nil {
		return err
	}
	mode := hdr.FileInfo().Mode().Perm()

	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, mode|0700)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			return err
		}
		if err := archive.WriteFile(target, tr, mode|0600); err != nil { //nolint:gosec // decompression size is bounded by the checksummed archive
			return err
		}
		return os.Chtimes(target, hdr.ModTime, hdr.ModTime)
	case tar.TypeSymlink:
		//nolint:gosec // this is the traversal check for the link's target
		if _, err := within(dest, filepath.Join(filepath.Dir(hdr.Name), hdr.Linkname)); err != nil || filepath.IsAbs(hdr.Linkname) {
			return fmt.Errorf("symlink %s points outside the archive", hdr.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			return err
		}
		return os.Symlink(hdr.Linkname, target)
	case tar.TypeLink:
		source, err := within(dest, hdr.Linkname)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			return err
		}
		return os.Link(source, target)
	}
	return nil
}

// within joins name onto dest, rejecting any name that would land outside it.
func within(dest, name string) (string, error) {
	target := filepath.Join(dest, filepath.FromSlash(name))
	if target != filepath.Clean(dest) && !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	return target, nil
}
