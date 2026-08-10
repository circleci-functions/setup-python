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

// requirementsTxt is the pip and pip-dist dependency file name, which also
// stands in as their lockfile for cache-key purposes.
const requirementsTxt = "requirements.txt"

// DetectFromDir scans dir for known Python project files and returns the
// matching package manager. setupFilePath, when non-empty, names a file
// relative to dir that marks a requirements.txt project as pip-dist rather
// than pip, the same way setup.py does.
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

// Lockfile returns the canonical lockfile name for pm, used for cache-key
// hashing and as the default dependency file. pip and pip-dist have no
// lockfile format, so requirements.txt fills that role for both.
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
