package pythondist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	hc "github.com/circleci-functions/toolkit/httpcl"
)

// archiveRoot is the single top-level directory in every build's archive.
const archiveRoot = "python"

// Install downloads d, checks it against its SHA-256, and unpacks it so that
// dest is the root of the install (holding bin/python3, or python.exe on
// Windows). Anything already at dest, such as an incomplete install, is
// replaced.
func Install(ctx context.Context, d Download, dest string) error {
	name := archiveName(d.URL)
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0750); err != nil {
		return err
	}
	archive, err := os.CreateTemp(parent, name+".*.download")
	if err != nil {
		return err
	}
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archive.Name())
	}()

	if err := download(ctx, d.URL, archive); err != nil {
		return err
	}
	if err := verify(archive, d.SHA256); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	staging, err := os.MkdirTemp(parent, filepath.Base(dest)+".*.extract")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()

	if err := extractTarGz(archive.Name(), staging); err != nil {
		return fmt.Errorf("extract %s: %w", name, err)
	}
	root := filepath.Join(staging, archiveRoot)
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("%s has no %s directory", name, archiveRoot)
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return os.Rename(root, dest)
}

// download writes the file at url to f, starting over on each retry.
func download(ctx context.Context, url string, f *os.File) error {
	client := hc.New(ctx, hc.Config{Name: "python-build-standalone", Timeout: 15 * time.Minute})
	err := client.Call(ctx, hc.NewRequest(http.MethodGet, "%s",
		hc.RouteParams(url),
		hc.SuccessDecoder(func(r io.Reader) error {
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return err
			}
			if err := f.Truncate(0); err != nil {
				return err
			}
			_, err := io.Copy(f, r)
			return err
		}),
		hc.Timeout(10*time.Minute),
	))
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	return nil
}

func verify(f *os.File, want string) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(want) {
		return fmt.Errorf("checksum mismatch: got sha256 %s, want %s", got, want)
	}
	return nil
}

// archiveName returns the file name url ends in, with its escapes decoded:
// python-build-standalone names carry a "+" that the URL escapes.
func archiveName(url string) string {
	name := path.Base(url)
	return strings.ReplaceAll(name, "%2B", "+")
}
