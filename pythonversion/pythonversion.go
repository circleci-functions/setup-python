// Package pythonversion resolves a Python version spec, such as "3.13",
// ">=3.10 <3.14" or "3.14-dev", to a concrete CPython release, and reads such
// specs from the files projects commonly keep them in.
package pythonversion

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// DefaultSpec is what to install when nothing asks for a version: the newest
// stable CPython 3 release.
const DefaultSpec = "3.x"

// ErrNoMatch is returned by Resolve when no release satisfies the spec.
var ErrNoMatch = errors.New("no Python release matches")

// Spec is a parsed version spec.
type Spec struct {
	raw          string
	exact        string
	constraint   *semver.Constraints
	prerelease   *semver.Constraints
	Freethreaded bool
}

var (
	unsupported      = regexp.MustCompile(`^(pypy|graalpy|pyston)`)
	minorOnly        = regexp.MustCompile(`^(\d+)\.(\d+)$`)
	pep440Pre        = regexp.MustCompile(`(\d+\.\d+\.\d+)-?(alpha|beta|a|b|c|rc)\.?(\d+)`)
	compatible       = regexp.MustCompile(`~=\s*(\d+)\.(\d+)(\.\d+)?`)
	exactVersion     = regexp.MustCompile(`^=*\s*v?(\d+\.\d+\.\d+(?:-(?:a|b|rc)\.\d+)?)$`)
	releaseVersion   = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:(a|b|rc)(\d+))?$`)
	semverPrerelease = regexp.MustCompile(`-(a|b|rc)\.(\d+)$`)
)

var prereleaseTags = map[string]string{"alpha": "a", "a": "a", "beta": "b", "b": "b", "c": "rc", "rc": "rc"}

// Parse parses spec, which is any of:
//
//   - an exact version, "3.13.1" or "3.15.0rc2"
//   - a partial version, "3", "3.13", "3.x" or "3.13.x"
//   - a range, in npm style (">=3.10 <3.14", "^3.11", "~3.12.1", "3.11 || 3.12")
//     or PEP 440 style (">=3.10,<3.14", "~=3.11", "==3.12.*")
//   - "x.y-dev", for the newest release of x.y including prereleases
//
// with an optional "t" suffix on the version ("3.13t") asking for a
// free-threaded build, as freethreaded does. allowPrereleases lets an "x.y"
// spec fall back to prereleases when x.y has no stable release yet.
func Parse(spec string, freethreaded, allowPrereleases bool) (Spec, error) {
	s := strings.ToLower(strings.TrimSpace(spec))
	if s == "" {
		return Spec{}, errors.New("empty Python version spec")
	}
	if unsupported.MatchString(s) {
		return Spec{}, fmt.Errorf("unsupported Python version spec %q: setup-python installs CPython only", spec)
	}

	dev := strings.HasSuffix(s, "-dev")
	s = strings.TrimSuffix(s, "-dev")
	if strings.HasSuffix(s, "t") {
		freethreaded = true
		s = strings.TrimSuffix(s, "t")
	}
	s = normalise(s)

	p := Spec{raw: spec, Freethreaded: freethreaded}
	if m := exactVersion.FindStringSubmatch(s); m != nil && !dev {
		p.exact = pep440(m[1])
	}

	if dev {
		m := minorOnly.FindStringSubmatch(s)
		if m == nil {
			return Spec{}, fmt.Errorf("invalid Python version spec %q: -dev takes a major.minor version, such as 3.14-dev", spec)
		}
		s = withPrereleases(m)
	}
	c, err := semver.NewConstraint(s)
	if err != nil {
		return Spec{}, fmt.Errorf("invalid Python version spec %q: %w", spec, err)
	}
	p.constraint = c

	if m := minorOnly.FindStringSubmatch(s); m != nil && allowPrereleases {
		p.prerelease, err = semver.NewConstraint(withPrereleases(m))
		if err != nil {
			return Spec{}, err
		}
	}
	return p, nil
}

// String returns the spec as it was given.
func (s Spec) String() string {
	return s.raw
}

// Exact returns the version an exact spec names, such as "3.13.1".
func (s Spec) Exact() (string, bool) {
	return s.exact, s.exact != ""
}

// Resolve returns the newest of versions that s matches. versions are CPython
// release versions as python --version reports them, such as "3.13.1" or
// "3.15.0rc2"; any that are not are ignored.
func (s Spec) Resolve(versions []string) (string, error) {
	type parsed struct {
		raw string
		v   *semver.Version
	}
	var sorted []parsed
	for _, raw := range versions {
		if v, err := semver.NewVersion(toSemver(raw)); err == nil {
			sorted = append(sorted, parsed{raw: raw, v: v})
		}
	}
	slices.SortFunc(sorted, func(a, b parsed) int { return b.v.Compare(a.v) })

	for _, c := range []*semver.Constraints{s.constraint, s.prerelease} {
		if c == nil {
			continue
		}
		for _, p := range sorted {
			if c.Check(p.v) {
				return p.raw, nil
			}
		}
	}
	return "", fmt.Errorf("%w %q", ErrNoMatch, s.raw)
}

// normalise rewrites the PEP 440 forms in spec as the npm-style ranges semver
// understands, and prerelease versions ("3.15.0rc2", "3.15.0-beta.1") as
// semver prereleases ("3.15.0-rc.2", "3.15.0-b.1"), which order a < b < rc as
// Python does.
func normalise(spec string) string {
	s := pep440Pre.ReplaceAllStringFunc(spec, func(v string) string {
		m := pep440Pre.FindStringSubmatch(v)
		return m[1] + "-" + prereleaseTags[m[2]] + "." + m[3]
	})
	s = compatible.ReplaceAllStringFunc(s, func(v string) string {
		m := compatible.FindStringSubmatch(v)
		if m[3] == "" {
			return "^" + m[1] + "." + m[2]
		}
		return "~" + m[1] + "." + m[2] + m[3]
	})
	s = strings.ReplaceAll(s, "===", "=")
	s = strings.ReplaceAll(s, "==", "=")
	return strings.ReplaceAll(s, ".*", ".x")
}

// withPrereleases returns a range matching every release of the major.minor
// version in m, prereleases included.
func withPrereleases(m []string) string {
	return "~" + m[1] + "." + m[2] + ".0-0"
}

// toSemver rewrites a CPython version as semver, "3.15.0rc2" as "3.15.0-rc.2".
func toSemver(version string) string {
	m := releaseVersion.FindStringSubmatch(version)
	if m == nil || m[4] == "" {
		return version
	}
	return m[1] + "." + m[2] + "." + m[3] + "-" + m[4] + "." + m[5]
}

// pep440 rewrites a semver version as CPython writes it, "3.15.0-rc.2" as
// "3.15.0rc2".
func pep440(version string) string {
	return semverPrerelease.ReplaceAllString(version, "$1$2")
}
