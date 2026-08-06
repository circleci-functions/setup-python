package python

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
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
	return addAlias(map[string]string{"python": "python3", "pip": "pip3"})
}

// addAlias appends a shell alias definition to $BASH_ENV for each name →
// command pair, so that later steps in the same job resolve name as
// command. Aliases are ignored by non-interactive shells unless
// expand_aliases is enabled, so this also enables it.
// Returns an error if $BASH_ENV is not set.
func addAlias(aliases map[string]string) error {
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString("shopt -s expand_aliases\n")
	for _, name := range names {
		fmt.Fprintf(&sb, "alias %s=%q\n", name, aliases[name])
	}
	return appendToBashEnv(sb.String())
}

// appendToBashEnv appends content to $BASH_ENV.
// Returns an error if $BASH_ENV is not set.
func appendToBashEnv(content string) error {
	bashEnv := os.Getenv("BASH_ENV")
	if bashEnv == "" {
		return fmt.Errorf("BASH_ENV must be set to configure")
	}

	f, err := os.OpenFile(bashEnv, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666) //nolint:gosec
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = f.WriteString(content)
	return err
}
