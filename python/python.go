package python

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/circleci-functions/toolkit/pathlink"
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
// to link the aliases.
func ResolveBinaries() Binaries {
	if _, err := exec.LookPath("python"); err == nil {
		return Binaries{Python: "python", Pip: "pip"}
	}
	return Binaries{Python: "python3", Pip: "pip3", NeedsAlias: true}
}

// EnsureAliased makes python and pip resolve to python3 and pip3 when
// b.NeedsAlias is set, so bare python and pip calls in later steps resolve.
// A link, not a bash alias: it carries into any shell. Returns nil when no
// alias is needed.
func EnsureAliased(b Binaries) error {
	return linkAliases(b, "", "")
}

// linkAliases links python3, pip3 and the python/pip aliases into a directory
// on PATH. dir links into exactly that directory and skips discovery, and path
// replaces the process's PATH; tests set both, so they cannot write to the
// real PATH.
func linkAliases(b Binaries, dir, path string) error {
	if !b.NeedsAlias {
		return nil
	}

	// Nothing to link when python3 is absent as well: NeedsAlias only says
	// `python` is missing. The install step reports having no python at all,
	// and reports it better than a failure to link would.
	python3, err := exec.LookPath("python3")
	if err != nil {
		return nil
	}

	_, err = pathlink.Ensure(pathlink.Options{
		Names:     []string{"python3", "pip3"},
		FromDir:   filepath.Dir(python3),
		Aliases:   map[string]string{"python": "python3", "pip": "pip3"},
		Preferred: pathlink.UserBinDirs(),
		Dir:       dir,
		Path:      path,
		Verify:    verifyPython,
	})
	return err
}

// verifyPython asks the resolved binary to identify itself, catching a link
// that resolves to something other than a python.
func verifyPython(bin string) error {
	out, err := pathlink.Command(bin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not report its version: %w", err)
	}
	if !strings.Contains(string(out), "Python") {
		return fmt.Errorf("reports %q, not a Python version", strings.TrimSpace(string(out)))
	}
	return nil
}
