package python

import (
	"os/exec"

	"github.com/circleci/functions/bashenv"
)

// Binaries holds the python and pip executable names to invoke, and whether
// later steps in the same job need a python/pip alias to resolve them.
type Binaries struct {
	Python     string
	Pip        string
	NeedsAlias bool
}

// ResolveBinaries returns the python and pip executables to invoke. It
// prefers python and pip, falling back to python3 and pip3 with NeedsAlias
// set when python is not on PATH. It has no side effects; call EnsureAliased
// to write the alias.
func ResolveBinaries() Binaries {
	if _, err := exec.LookPath("python"); err == nil {
		return Binaries{Python: "python", Pip: "pip"}
	}
	return Binaries{Python: "python3", Pip: "pip3", NeedsAlias: true}
}

// EnsureAliased writes a python/pip to python3/pip3 alias to $BASH_ENV when
// b.NeedsAlias is set, so bare python and pip calls in later steps resolve.
// Returns nil when no alias is needed.
func EnsureAliased(b Binaries) error {
	if !b.NeedsAlias {
		return nil
	}
	return bashenv.AddAlias(map[string]string{"python": "python3", "pip": "pip3"})
}
