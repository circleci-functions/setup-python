// Package python implements shared logic for the setup-python function:
// package manager detection, install command construction, and (in later
// files) cache path/key computation.
package python

import (
	"fmt"
	"os"
	"path/filepath"
)

// PackageManager is a supported Python package manager.
type PackageManager string

const (
	Pip     PackageManager = "pip"
	PipDist PackageManager = "pip-dist"
	Pipenv  PackageManager = "pipenv"
	Poetry  PackageManager = "poetry"
	UV      PackageManager = "uv"
)

// requirementsTxt is the pip/pip-dist dependency file name. It also doubles
// as the pip/pip-dist "lockfile" for cache-key purposes (there's no real
// lockfile mechanism for plain pip).
const requirementsTxt = "requirements.txt"

// DetectFromDir scans dir for known Python project files and returns the
// matching package manager, mirroring the circleci/python orb's
// `pkg-manager: auto` detection (see porting.md). setupFilePath is the
// value of the --setup-file-path flag: when non-empty it names an
// additional file (relative to dir) that, like setup.py, marks a
// requirements.txt-based project as pip-dist rather than plain pip. Pass ""
// when the flag wasn't set.
//
// Priority order (first match wins):
//  1. requirements.txt + (setup.py or setupFilePath present) -> pip-dist
//  2. requirements.txt alone                                 -> pip
//  3. Pipfile                                                 -> pipenv
//  4. uv.lock                                                 -> uv
//  5. pyproject.toml                                          -> poetry
//
// Returns an error if none of the above match.
func DetectFromDir(dir string, setupFilePath string) (PackageManager, error) {
	exists := func(name string) bool {
		if name == "" {
			return false
		}
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}

	if exists(requirementsTxt) {
		if exists("setup.py") || exists(setupFilePath) {
			return PipDist, nil
		}
		return Pip, nil
	}
	if exists("Pipfile") {
		return Pipenv, nil
	}
	if exists("uv.lock") {
		return UV, nil
	}
	if exists("pyproject.toml") {
		return Poetry, nil
	}

	return "", fmt.Errorf("no package manager detected in %s", dir)
}

// Lockfile returns the canonical lockfile name for pm, used both for
// cache-key hashing and as the default dependency file. pip and pip-dist
// have no real lockfile format; requirements.txt fills that role for both
// (see porting.md's install-packages cache table).
func (pm PackageManager) Lockfile() string {
	switch pm {
	case Pip, PipDist:
		return requirementsTxt
	case Pipenv:
		return "Pipfile.lock"
	case UV:
		return "uv.lock"
	case Poetry:
		return "poetry.lock"
	default:
		return ""
	}
}

// Binary returns the name of the pm executable. pip-dist installs via the
// pip binary, same as pip.
func (pm PackageManager) Binary() string {
	switch pm {
	case Pip, PipDist:
		return "pip"
	default:
		return string(pm)
	}
}

// InstallArgs returns the argv (excluding the binary name) for pm's install
// command, matching the circleci/python orb's install-packages command:
//
//	pip     -> pip install -r <dependencyFile> <args>
//	pip-dist-> pip install -e <pathArgs> <args>
//	pipenv  -> pipenv install <args>
//	poetry  -> poetry install --no-ansi <args>
//	uv      -> uv sync <args>
//
// dependencyFile is only consulted for Pip (the orb's --pip-dependency-file);
// pathArgs is only consulted for PipDist (the orb's --path-args). args are
// extra arguments (the orb's --args, already split into fields) appended
// verbatim to every manager's command. Returns nil for an unknown pm.
func (pm PackageManager) InstallArgs(dependencyFile, pathArgs string, args []string) []string {
	var out []string
	switch pm {
	case Pip:
		out = []string{"install", "-r", dependencyFile}
	case PipDist:
		out = []string{"install", "-e", pathArgs}
	case Pipenv:
		out = []string{"install"}
	case Poetry:
		out = []string{"install", "--no-ansi"}
	case UV:
		out = []string{"sync"}
	default:
		return nil
	}
	return append(out, args...)
}
