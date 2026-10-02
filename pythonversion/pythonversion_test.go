package pythonversion

import (
	"testing"

	"gotest.tools/v3/assert"
)

var versions = []string{
	"3.12.9", "3.12.10",
	"3.13.0", "3.13.1", "3.13.2",
	"3.14.0a1", "3.14.0b2", "3.14.0rc1", "3.14.0rc10", "3.14.0", "3.14.1",
	"3.15.0a3", "3.15.0b1", "3.15.0rc2",
}

func TestResolve(t *testing.T) {
	tests := []struct {
		spec             string
		allowPrereleases bool
		want             string
		wantFreethreaded bool
	}{
		{spec: "3.13.1", want: "3.13.1"},
		{spec: "v3.13.1", want: "3.13.1"},
		{spec: "==3.13.1", want: "3.13.1"},
		{spec: "3.13", want: "3.13.2"},
		{spec: "3.12", want: "3.12.10"},
		{spec: "3", want: "3.14.1"},
		{spec: "3.x", want: "3.14.1"},
		{spec: "3.13.x", want: "3.13.2"},
		{spec: " 3.13 ", want: "3.13.2"},
		{spec: ">=3.12 <3.14", want: "3.13.2"},
		{spec: ">=3.12,<3.14", want: "3.13.2"},
		{spec: ">=3.12, <3.14", want: "3.13.2"},
		{spec: "^3.12", want: "3.14.1"},
		{spec: "~3.13.0", want: "3.13.2"},
		{spec: "~=3.12", want: "3.14.1"},
		{spec: "~=3.13.1", want: "3.13.2"},
		{spec: "==3.12.*", want: "3.12.10"},
		{spec: "3.12 || 3.13", want: "3.13.2"},
		{spec: "!=3.14.1", want: "3.14.0"},
		{spec: "3.15.0rc2", want: "3.15.0rc2"},
		{spec: "3.15.0-rc.2", want: "3.15.0rc2"},
		{spec: "3.15.0-beta.1", want: "3.15.0b1"},
		{spec: "3.15.0a3", want: "3.15.0a3"},
		{spec: "3.14-dev", want: "3.14.1"},
		{spec: "3.15-dev", want: "3.15.0rc2"},
		{spec: "3.15", allowPrereleases: true, want: "3.15.0rc2"},
		{spec: "3.14", allowPrereleases: true, want: "3.14.1"},
		{spec: "3.13t", want: "3.13.2", wantFreethreaded: true},
		{spec: "3.13.1t", want: "3.13.1", wantFreethreaded: true},
		{spec: "3.15t-dev", want: "3.15.0rc2", wantFreethreaded: true},
	}
	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			s, err := Parse(tc.spec, false, tc.allowPrereleases)
			assert.NilError(t, err)
			got, err := s.Resolve(versions)
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
			assert.Equal(t, s.Freethreaded, tc.wantFreethreaded)
		})
	}
}

func TestResolve_PrereleasesOrderLikePython(t *testing.T) {
	s, err := Parse("3.14-dev", false, false)
	assert.NilError(t, err)
	got, err := s.Resolve([]string{"3.14.0a1", "3.14.0rc10", "3.14.0rc2", "3.14.0b3"})
	assert.NilError(t, err)
	assert.Equal(t, got, "3.14.0rc10")
}

func TestResolve_NoMatch(t *testing.T) {
	tests := []struct {
		spec             string
		allowPrereleases bool
	}{
		{spec: "3.11"},
		{spec: "3.15"},
		{spec: ">=3.15"},
		{spec: ">=3.15", allowPrereleases: true},
		{spec: "3.13.9"},
	}
	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			s, err := Parse(tc.spec, false, tc.allowPrereleases)
			assert.NilError(t, err)
			_, err = s.Resolve(versions)
			assert.ErrorIs(t, err, ErrNoMatch)
			assert.ErrorContains(t, err, `no Python release matches "`+tc.spec+`"`)
		})
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		spec    string
		wantErr string
	}{
		{spec: "", wantErr: "empty Python version spec"},
		{spec: "banana", wantErr: `invalid Python version spec "banana"`},
		{spec: "pypy3.10", wantErr: "installs CPython only"},
		{spec: "graalpy-24.1", wantErr: "installs CPython only"},
		{spec: "3.14.1-dev", wantErr: "-dev takes a major.minor version"},
	}
	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			_, err := Parse(tc.spec, false, false)
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestParse_Freethreaded(t *testing.T) {
	s, err := Parse("3.13", true, false)
	assert.NilError(t, err)
	assert.Assert(t, s.Freethreaded)
}

func TestExact(t *testing.T) {
	tests := []struct {
		spec string
		want string
	}{
		{spec: "3.13.1", want: "3.13.1"},
		{spec: "=3.13.1", want: "3.13.1"},
		{spec: "3.13.1t", want: "3.13.1"},
		{spec: "3.15.0rc2", want: "3.15.0rc2"},
		{spec: "3.15.0-beta.1", want: "3.15.0b1"},
		{spec: "3.13"},
		{spec: ">=3.13.1"},
		{spec: "3.14-dev"},
	}
	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			s, err := Parse(tc.spec, false, false)
			assert.NilError(t, err)
			got, ok := s.Exact()
			assert.Equal(t, got, tc.want)
			assert.Equal(t, ok, tc.want != "")
		})
	}
}
