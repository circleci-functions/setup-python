package python

import (
	"os/exec"

	"github.com/circleci/functions/bashenv"
)

// Binaries holds the resolved names of the python/pip executables to use
// for the function's own subprocess calls, plus whether later steps in the
// same job need a python/pip alias (see porting.md's "Python/pip binary
// resolution" section, principle 5 in the porting notes: direct resolution
// for our own exec calls, bashenv.AddAlias for everyone else's).
type Binaries struct {
	Python string
	Pip    string
	// NeedsAlias is true when "python" wasn't found on PATH, mirroring the
	// orb which only checks for "python" being missing (not "pip"
	// independently).
	NeedsAlias bool
}

// ResolveBinaries determines which python/pip executables to invoke.
// It prefers "python"/"pip" on PATH; if "python" isn't found, it falls back
// to "python3"/"pip3" and reports that an alias is needed for later steps.
// Pure function: performs no $BASH_ENV side effects, so it's independently
// testable. Use EnsureAliased to apply the side effect when needed.
func ResolveBinaries() Binaries {
	if _, err := exec.LookPath("python"); err == nil {
		return Binaries{Python: "python", Pip: "pip"}
	}
	return Binaries{Python: "python3", Pip: "pip3", NeedsAlias: true}
}

// EnsureAliased writes a python/pip -> python3/pip3 alias to $BASH_ENV via
// bashenv.AddAlias when b.NeedsAlias is set, so bare python/pip calls in
// later steps of the same job resolve correctly. It's a no-op (returns nil)
// when no alias is needed.
func EnsureAliased(b Binaries) error {
	if !b.NeedsAlias {
		return nil
	}
	return bashenv.AddAlias(map[string]string{"python": "python3", "pip": "pip3"})
}
