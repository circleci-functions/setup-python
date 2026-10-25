package python_test

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-python/python"
)

// TestDetectFromDir verifies which package manager is inferred from the files
// present in a project directory, including the priority order between them
// and the error when nothing matches.
func TestDetectFromDir(t *testing.T) {
	tests := []struct {
		name          string
		files         []string
		setupFilePath string
		wantPM        python.PackageManager
		wantErr       string
	}{
		{
			name:   "requirements.txt + setup.py -> pip-dist",
			files:  []string{"requirements.txt", "setup.py"},
			wantPM: python.PipDist,
		},
		{
			name:          "requirements.txt + custom setup-file-path -> pip-dist",
			files:         []string{"requirements.txt", "pkg-setup.py"},
			setupFilePath: "pkg-setup.py",
			wantPM:        python.PipDist,
		},
		{
			name:   "requirements.txt alone -> pip",
			files:  []string{"requirements.txt"},
			wantPM: python.Pip,
		},
		{
			name:   "requirements.txt + pyproject.toml (no setup.py) -> pip",
			files:  []string{"requirements.txt", "pyproject.toml"},
			wantPM: python.Pip,
		},
		{
			name:          "requirements.txt + setup-file-path set but file missing -> pip",
			files:         []string{"requirements.txt"},
			setupFilePath: "pkg-setup.py",
			wantPM:        python.Pip,
		},
		{
			name:   "Pipfile -> pipenv",
			files:  []string{"Pipfile"},
			wantPM: python.Pipenv,
		},
		{
			name:   "uv.lock -> uv",
			files:  []string{"uv.lock"},
			wantPM: python.UV,
		},
		{
			name:   "pyproject.toml alone -> poetry",
			files:  []string{"pyproject.toml"},
			wantPM: python.Poetry,
		},
		{
			name:   "pip-dist takes priority over pipenv/uv/poetry",
			files:  []string{"requirements.txt", "setup.py", "Pipfile", "uv.lock", "pyproject.toml"},
			wantPM: python.PipDist,
		},
		{
			name:   "pipenv takes priority over uv/poetry",
			files:  []string{"Pipfile", "uv.lock", "pyproject.toml"},
			wantPM: python.Pipenv,
		},
		{
			name:   "uv takes priority over poetry",
			files:  []string{"uv.lock", "pyproject.toml"},
			wantPM: python.UV,
		},
		{
			name:    "no manager detected",
			files:   nil,
			wantErr: "no package manager detected",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				assert.NilError(t, os.WriteFile(filepath.Join(dir, f), []byte(""), 0600))
			}

			pm, err := python.DetectFromDir(dir, tc.setupFilePath)

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, pm, tc.wantPM)
		})
	}
}

// TestLockfile verifies the lockfile name each package manager hashes for
// cache keys; pip and pip-dist have no lockfile format, so requirements.txt
// stands in for both.
func TestLockfile(t *testing.T) {
	tests := []struct {
		name string
		pm   python.PackageManager
		want string
	}{
		{name: "pip", pm: python.Pip, want: "requirements.txt"},
		{name: "pip-dist", pm: python.PipDist, want: "requirements.txt"},
		{name: "pipenv", pm: python.Pipenv, want: "Pipfile.lock"},
		{name: "uv", pm: python.UV, want: "uv.lock"},
		{name: "poetry", pm: python.Poetry, want: "poetry.lock"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.pm.Lockfile(), tc.want)
		})
	}
}

// TestBinary verifies the executable name each package manager installs with;
// pip-dist installs via the pip binary rather than one of its own.
func TestBinary(t *testing.T) {
	tests := []struct {
		name string
		pm   python.PackageManager
		want string
	}{
		{name: "pip", pm: python.Pip, want: "pip"},
		{name: "pip-dist", pm: python.PipDist, want: "pip"},
		{name: "pipenv", pm: python.Pipenv, want: "pipenv"},
		{name: "poetry", pm: python.Poetry, want: "poetry"},
		{name: "uv", pm: python.UV, want: "uv"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.pm.Binary(), tc.want)
		})
	}
}

// TestInstallArgs verifies the argv each package manager installs with, that
// only pip consults the dependency file and only pip-dist the path args, and
// that extra args are appended verbatim in every case.
func TestInstallArgs(t *testing.T) {
	tests := []struct {
		name           string
		pm             python.PackageManager
		dependencyFile string
		pathArgs       string
		args           []string
		want           []string
	}{
		{
			name:           "pip",
			pm:             python.Pip,
			dependencyFile: "requirements.txt",
			want:           []string{"install", "-r", "requirements.txt"},
		},
		{
			name:           "pip with extra args",
			pm:             python.Pip,
			dependencyFile: "reqs/dev.txt",
			args:           []string{"--no-cache-dir"},
			want:           []string{"install", "-r", "reqs/dev.txt", "--no-cache-dir"},
		},
		{
			name:     "pip-dist",
			pm:       python.PipDist,
			pathArgs: ".",
			want:     []string{"install", "-e", "."},
		},
		{
			name:     "pip-dist with extra args",
			pm:       python.PipDist,
			pathArgs: "./pkg",
			args:     []string{"--no-deps"},
			want:     []string{"install", "-e", "./pkg", "--no-deps"},
		},
		{
			name: "pipenv",
			pm:   python.Pipenv,
			want: []string{"install"},
		},
		{
			name: "pipenv with extra args",
			pm:   python.Pipenv,
			args: []string{"--dev"},
			want: []string{"install", "--dev"},
		},
		{
			name: "poetry",
			pm:   python.Poetry,
			want: []string{"install", "--no-ansi"},
		},
		{
			name: "poetry with extra args",
			pm:   python.Poetry,
			args: []string{"--no-root"},
			want: []string{"install", "--no-ansi", "--no-root"},
		},
		{
			name: "uv",
			pm:   python.UV,
			want: []string{"sync"},
		},
		{
			name: "uv with extra args",
			pm:   python.UV,
			args: []string{"--frozen"},
			want: []string{"sync", "--frozen"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pm.InstallArgs(tc.dependencyFile, tc.pathArgs, tc.args)
			assert.DeepEqual(t, got, tc.want)
		})
	}
}

// TestRequirement verifies the pip requirement for each package manager,
// with the version pyproject.toml pins for it, if any.
func TestRequirement(t *testing.T) {
	tests := []struct {
		name      string
		pm        python.PackageManager
		pyproject string
		want      string
	}{
		{
			name: "no pyproject.toml",
			pm:   python.UV,
			want: "uv",
		},
		{
			name:      "uv required-version",
			pm:        python.UV,
			pyproject: "[tool.uv]\nrequired-version = \">=0.5\"\n",
			want:      "uv>=0.5",
		},
		{
			name:      "poetry requires-poetry",
			pm:        python.Poetry,
			pyproject: "[tool.poetry]\nrequires-poetry = \">=2.0,<3\"\n",
			want:      "poetry>=2.0,<3",
		},
		{
			name:      "a bare version pins exactly",
			pm:        python.Poetry,
			pyproject: "[tool.poetry]\nrequires-poetry = \"2.1.3\"\n",
			want:      "poetry==2.1.3",
		},
		{
			name:      "another manager's pin is ignored",
			pm:        python.Poetry,
			pyproject: "[tool.uv]\nrequired-version = \">=0.5\"\n",
			want:      "poetry",
		},
		{
			name:      "pipenv has no pin",
			pm:        python.Pipenv,
			pyproject: "[tool.uv]\nrequired-version = \">=0.5\"\n",
			want:      "pipenv",
		},
		{
			name:      "invalid pyproject.toml",
			pm:        python.UV,
			pyproject: "[tool.uv",
			want:      "uv",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.pyproject != "" {
				assert.NilError(t, os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(tc.pyproject), 0600))
			}
			assert.Equal(t, tc.pm.Requirement(dir), tc.want)
		})
	}
}
