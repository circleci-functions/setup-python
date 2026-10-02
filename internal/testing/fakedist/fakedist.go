// Package fakedist serves a stand-in for uv's download metadata and the
// python-build-standalone archives it lists, so tests can resolve, download
// and unpack Python builds without the network.
package fakedist

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/circleci-functions/setup-python/pythondist"
)

// MetadataPath is where the server serves its download metadata.
const MetadataPath = "/download-metadata.json"

// Release is one CPython release the server lists a build of, for Platform.
type Release struct {
	// Version is as python --version reports it, e.g. 3.13.1 or 3.15.0rc2.
	Version      string
	Freethreaded bool
	// Platform defaults to the host.
	Platform pythondist.Platform
}

// Server is a running fake distribution server.
type Server struct {
	URL string

	host     pythondist.Platform
	releases []Release
	mu       sync.Mutex
	requests []*http.Request
	corrupt  bool
}

// New starts a server listing releases, and stops it when t ends.
func New(t *testing.T, releases ...Release) *Server {
	t.Helper()
	host, err := pythondist.Host()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{host: host}
	for _, rel := range releases {
		s.releases = append(s.releases, s.withDefaults(rel))
	}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

func (s *Server) withDefaults(rel Release) Release {
	if rel.Platform == (pythondist.Platform{}) {
		rel.Platform = s.host
	}
	return rel
}

// MetadataURL is the URL of the server's download metadata.
func (s *Server) MetadataURL() string {
	return s.URL + MetadataPath
}

// Corrupt makes the server send archives that do not match their checksums.
func (s *Server) Corrupt() {
	s.corrupt = true
}

// Requests returns the requests served so far.
func (s *Server) Requests() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.requests...)
}

// Downloads returns how many archives have been served.
func (s *Server) Downloads() int {
	n := 0
	for _, r := range s.Requests() {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			n++
		}
	}
	return n
}

// MetadataFetches returns how many times the download metadata was served.
func (s *Server) MetadataFetches() int {
	n := 0
	for _, r := range s.Requests() {
		if r.URL.Path == MetadataPath {
			n++
		}
	}
	return n
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.Clone(r.Context()))
	s.mu.Unlock()

	if r.URL.Path == MetadataPath {
		s.serveMetadata(w)
		return
	}
	for _, rel := range s.releases {
		if "/"+archiveName(rel) != r.URL.Path {
			continue
		}
		data := Archive(rel)
		if s.corrupt {
			data = append(data, 0)
		}
		_, _ = w.Write(data)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) serveMetadata(w http.ResponseWriter) {
	entries := map[string]pythondist.Download{}
	for _, rel := range s.releases {
		d := s.Download(rel)
		entries[fmt.Sprintf("cpython-%s-%s-%s-%s-%s", rel.Version, d.Variant, d.OS, d.Arch.Family, d.Libc)] = d
	}
	_ = json.NewEncoder(w).Encode(entries)
}

var version = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(.*)$`)

// Download returns the metadata entry the server lists for rel.
func (s *Server) Download(rel Release) pythondist.Download {
	rel = s.withDefaults(rel)
	m := version.FindStringSubmatch(rel.Version)
	number := func(i int) int {
		n, _ := strconv.Atoi(m[i])
		return n
	}
	sum := sha256.Sum256(Archive(rel))
	d := pythondist.Download{
		Name:       "cpython",
		Arch:       pythondist.Arch{Family: rel.Platform.Arch},
		OS:         rel.Platform.OS,
		Libc:       rel.Platform.Libc,
		Major:      number(1),
		Minor:      number(2),
		Patch:      number(3),
		Prerelease: m[4],
		URL:        s.URL + "/" + archiveName(rel),
		SHA256:     hex.EncodeToString(sum[:]),
		Build:      "20260101",
	}
	if rel.Freethreaded {
		d.Variant = "freethreaded"
	}
	return d
}

func archiveName(rel Release) string {
	variant := ""
	if rel.Freethreaded {
		variant = "-freethreaded"
	}
	return fmt.Sprintf("cpython-%s-%s%s-install_only_stripped.tar.gz", rel.Version, rel.Platform, variant)
}

// Archive returns the archive of rel's build, laid out like a real one under
// a top-level python directory. On Unix, bin/python3.X is a shell script that
// answers --version, reports $HOME/.local/bin as pip's user scripts directory,
// and appends any -m invocation to $STUB_LOG, so a test can run it.
func Archive(rel Release) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	file := func(name, content string, mode int64) {
		_ = tw.WriteHeader(&tar.Header{Name: "python/" + name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(content))})
		_, _ = tw.Write([]byte(content))
	}
	link := func(name, target string) {
		_ = tw.WriteHeader(&tar.Header{Name: "python/" + name, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0777})
	}
	dir := func(name string) {
		_ = tw.WriteHeader(&tar.Header{Name: "python/" + name, Typeflag: tar.TypeDir, Mode: 0755})
	}

	m := version.FindStringSubmatch(rel.Version)
	minor := m[1] + "." + m[2]
	dir("")
	if rel.Platform.OS == "windows" {
		file("python.exe", "Python "+rel.Version, 0755)
		file("python"+m[1]+m[2]+".dll", "", 0644)
		dir("Scripts/")
		dir("Lib/site-packages/pip/")
	} else {
		exe := "python" + minor
		if rel.Freethreaded {
			exe += "t"
			link("bin/python"+minor, exe)
		}
		dir("bin/")
		file("bin/"+exe, pythonScript(rel.Version), 0755)
		link("bin/python", "python"+minor)
		link("bin/python3", "python"+minor)
		for _, pip := range []string{"pip", "pip3", "pip" + minor} {
			file("bin/"+pip, "#!/bin/sh\nexec \"$(dirname \"$0\")/python"+minor+"\" -m pip \"$@\"\n", 0755)
		}
		dir("lib/python" + minor + "/site-packages/pip/")
	}

	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func pythonScript(version string) string {
	return `#!/bin/sh
case "$1" in
--version) echo "Python ` + version + `" ;;
-c) echo "$HOME/.local/bin" ;;
-m) [ -n "$STUB_LOG" ] && printf 'python %s\n' "$*" >> "$STUB_LOG" ;;
esac
exit 0
`
}
