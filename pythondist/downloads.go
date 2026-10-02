package pythondist

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	hc "github.com/circleci-functions/toolkit/httpcl"
)

// DefaultURL is uv's list of the Python builds it can install, kept current
// as python-build-standalone publishes them.
const DefaultURL = "https://raw.githubusercontent.com/astral-sh/uv/main/crates/uv-python/download-metadata.json"

//go:embed downloads.json
var snapshot []byte

// Download is one build in uv's download metadata.
type Download struct {
	Name       string `json:"name"`
	Arch       Arch   `json:"arch"`
	OS         string `json:"os"`
	Libc       string `json:"libc"`
	Major      int    `json:"major"`
	Minor      int    `json:"minor"`
	Patch      int    `json:"patch"`
	Prerelease string `json:"prerelease"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	Variant    string `json:"variant"`
	Build      string `json:"build"`
}

// Arch is a build's architecture. Variant names a microarchitecture level,
// such as v3, for builds that need a newer CPU than the family's baseline.
type Arch struct {
	Family  string `json:"family"`
	Variant string `json:"variant"`
}

// Version returns the build's CPython version as python --version reports
// it, e.g. 3.13.1 or 3.15.0rc2.
func (d Download) Version() string {
	return fmt.Sprintf("%d.%d.%d%s", d.Major, d.Minor, d.Patch, d.Prerelease)
}

// Freethreaded reports whether the build is free-threaded.
func (d Download) Freethreaded() bool {
	return d.Variant == "freethreaded"
}

func (d Download) matches(p Platform) bool {
	return d.OS == p.OS && d.Arch.Family == p.Arch && d.Libc == p.Libc
}

// installable reports whether setup-python can install d: a baseline CPython
// build of the default or free-threaded flavour, packaged as a checksummed
// tar.gz.
func (d Download) installable() bool {
	return d.Name == "cpython" &&
		d.Arch.Variant == "" &&
		(d.Variant == "" || d.Freethreaded()) &&
		strings.HasSuffix(d.URL, ".tar.gz") &&
		d.SHA256 != "" &&
		slices.Contains([]string{"linux", "darwin", "windows"}, d.OS)
}

// Catalog is a list of installable builds.
type Catalog map[string]Download

// Parse reads uv's download metadata, keeping only the builds setup-python
// can install.
func Parse(data []byte) (Catalog, error) {
	var all map[string]Download
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("parse Python download metadata: %w", err)
	}
	c := Catalog{}
	for key, d := range all {
		if d.installable() {
			c[key] = d
		}
	}
	return c, nil
}

// Snapshot returns the catalog built into setup-python, taken from uv's list
// when this release of setup-python was made.
func Snapshot() (Catalog, error) {
	return Parse(snapshot)
}

// Fetch downloads and parses the download metadata at url.
func Fetch(ctx context.Context, url string) (Catalog, error) {
	client := hc.New(ctx, hc.Config{Name: "python-downloads", Timeout: 2 * time.Minute})
	var data []byte
	err := client.Call(ctx, hc.NewRequest(http.MethodGet, "%s",
		hc.RouteParams(url),
		hc.BytesDecoder(&data),
		hc.Timeout(time.Minute),
	))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	return Parse(data)
}

// For returns the builds in c for p, of the free-threaded flavour or not.
func (c Catalog) For(p Platform, freethreaded bool) Catalog {
	out := Catalog{}
	for key, d := range c {
		if d.matches(p) && d.Freethreaded() == freethreaded {
			out[key] = d
		}
	}
	return out
}

// Versions returns the versions c holds builds of.
func (c Catalog) Versions() []string {
	versions := make([]string, 0, len(c))
	for _, d := range c {
		versions = append(versions, d.Version())
	}
	slices.Sort(versions)
	return slices.Compact(versions)
}

// Find returns the build of version in c. Given builds of more than one
// platform or flavour, which it returns is unspecified; narrow c with For
// first.
func (c Catalog) Find(version string) (Download, bool) {
	for _, d := range c {
		if d.Version() == version {
			return d, true
		}
	}
	return Download{}, false
}

// Marshal writes c as uv's download metadata, one build to a line in key
// order, so a refreshed snapshot diffs by build.
func (c Catalog) Marshal() ([]byte, error) {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, key := range keys {
		k, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(c[key])
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteString(": ")
		buf.Write(v)
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}
