package pythonversion

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultFile is the version file read when no version or version file is
// given.
const DefaultFile = ".python-version"

// FromFile reads a version spec from path, by the file's name:
//
//   - .tool-versions: the python line
//   - pyproject.toml: project.requires-python, then
//     tool.poetry.dependencies.python
//   - Pipfile: requires.python_full_version, then requires.python_version
//   - anything else, such as .python-version: the first line that is not a
//     comment
func FromFile(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the caller names the file to read
	if err != nil {
		return "", err
	}

	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	var spec string
	switch filepath.Base(path) {
	case ".tool-versions":
		spec = fromToolVersions(string(data))
	case "pyproject.toml":
		spec, err = fromPyproject(data)
	case "Pipfile":
		spec, err = fromPipfile(data)
	default:
		spec = firstLine(string(data))
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if spec == "" {
		return "", fmt.Errorf("%s: %w", path, ErrNoVersion)
	}
	return spec, nil
}

// ErrNoVersion is returned by FromFile for a file that names no version.
var ErrNoVersion = errors.New("no Python version found")

// Default reads the spec from DefaultFile in dir, returning the file it read,
// or an empty spec and source when dir has none or it names no version.
func Default(dir string) (spec, source string, err error) {
	path := filepath.Join(dir, DefaultFile)
	spec, err = FromFile(path)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrNoVersion) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return spec, path, nil
}

func firstLine(data string) string {
	for line := range strings.Lines(data) {
		if fields := strings.Fields(line); len(fields) > 0 && !strings.HasPrefix(fields[0], "#") {
			return fields[0]
		}
	}
	return ""
}

func fromToolVersions(data string) string {
	for line := range strings.Lines(data) {
		if fields := strings.Fields(line); len(fields) > 1 && fields[0] == "python" {
			return fields[1]
		}
	}
	return ""
}

func fromPyproject(data []byte) (string, error) {
	var doc struct {
		Project struct {
			RequiresPython string `toml:"requires-python"`
		} `toml:"project"`
		Tool struct {
			Poetry struct {
				Dependencies map[string]any `toml:"dependencies"`
			} `toml:"poetry"`
		} `toml:"tool"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	if doc.Project.RequiresPython != "" {
		return padPEP440(doc.Project.RequiresPython), nil
	}
	python, _ := doc.Tool.Poetry.Dependencies["python"].(string)
	return python, nil
}

func fromPipfile(data []byte) (string, error) {
	var doc struct {
		Requires struct {
			PythonFullVersion string `toml:"python_full_version"`
			PythonVersion     string `toml:"python_version"`
		} `toml:"requires"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	if doc.Requires.PythonFullVersion != "" {
		return doc.Requires.PythonFullVersion, nil
	}
	return doc.Requires.PythonVersion, nil
}

var pep440Clause = regexp.MustCompile(`(===|==|!=|<=|>=|~=|<|>)\s*(\d+(?:\.\d+)*(?:\.\*)?)`)

// padPEP440 pads the major.minor versions in a PEP 440 specifier to
// major.minor.0 wherever PEP 440 compares them as such and npm-style ranges
// would not: ">3.12" admits 3.12.1 in PEP 440 and "==3.12" and "<=3.12" mean
// 3.12.0, where npm reads them as whole minor versions.
func padPEP440(spec string) string {
	return pep440Clause.ReplaceAllStringFunc(spec, func(clause string) string {
		m := pep440Clause.FindStringSubmatch(clause)
		op, version := m[1], m[2]
		switch op {
		case ">", "<=", "==", "===", "!=":
		default:
			return clause
		}
		if strings.HasSuffix(version, ".*") || strings.Count(version, ".") != 1 {
			return clause
		}
		return op + version + ".0"
	})
}
