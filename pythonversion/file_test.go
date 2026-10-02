package pythonversion

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestFromFile(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		want    string
	}{
		{name: "python-version", file: ".python-version", content: "3.12.1\n", want: "3.12.1"},
		{name: "python-version comments and blanks", file: ".python-version", content: "# pinned\n\n  3.13  \n3.12\n", want: "3.13"},
		{name: "python-version crlf", file: ".python-version", content: "3.13\r\n", want: "3.13"},
		{name: "python-version byte order mark", file: ".python-version", content: "\ufeff3.13\n", want: "3.13"},
		{name: "any other name", file: "python.txt", content: "3.11\n", want: "3.11"},
		{name: "tool-versions", file: ".tool-versions", content: "nodejs 22.1.0\npython 3.12.1 3.11.4\n", want: "3.12.1"},
		{name: "pyproject requires-python", file: "pyproject.toml", content: "[project]\nname = \"x\"\nrequires-python = \">=3.10,<3.14\"\n", want: ">=3.10,<3.14"},
		{name: "pyproject poetry", file: "pyproject.toml", content: "[tool.poetry.dependencies]\npython = \"^3.11\"\nrequests = \"^2\"\n", want: "^3.11"},
		{name: "pyproject prefers requires-python", file: "pyproject.toml", content: "[project]\nrequires-python = \">=3.12\"\n[tool.poetry.dependencies]\npython = \"^3.11\"\n", want: ">=3.12"},
		{name: "pipfile full version", file: "Pipfile", content: "[requires]\npython_version = \"3.12\"\npython_full_version = \"3.12.4\"\n", want: "3.12.4"},
		{name: "pipfile version", file: "Pipfile", content: "[requires]\npython_version = \"3.12\"\n", want: "3.12"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.file)
			assert.NilError(t, os.WriteFile(path, []byte(tc.content), 0600))
			got, err := FromFile(path)
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestFromFile_Errors(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		wantErr string
	}{
		{name: "empty", file: ".python-version", content: "# nothing\n", wantErr: "no Python version found"},
		{name: "tool-versions without python", file: ".tool-versions", content: "nodejs 22\n", wantErr: "no Python version found"},
		{name: "pyproject without a version", file: "pyproject.toml", content: "[project]\nname = \"x\"\n", wantErr: "no Python version found"},
		{name: "bad toml", file: "pyproject.toml", content: "[project\n", wantErr: "pyproject.toml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.file)
			assert.NilError(t, os.WriteFile(path, []byte(tc.content), 0600))
			_, err := FromFile(path)
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestFromFile_Missing(t *testing.T) {
	_, err := FromFile(filepath.Join(t.TempDir(), ".python-version"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestDefault(t *testing.T) {
	dir := t.TempDir()
	spec, source, err := Default(dir)
	assert.NilError(t, err)
	assert.Equal(t, spec, "")
	assert.Equal(t, source, "")

	assert.NilError(t, os.WriteFile(filepath.Join(dir, ".python-version"), []byte("# none yet\n"), 0600))
	spec, source, err = Default(dir)
	assert.NilError(t, err)
	assert.Equal(t, spec, "")
	assert.Equal(t, source, "")

	assert.NilError(t, os.WriteFile(filepath.Join(dir, ".python-version"), []byte("3.13\n"), 0600))
	spec, source, err = Default(dir)
	assert.NilError(t, err)
	assert.Equal(t, spec, "3.13")
	assert.Equal(t, source, filepath.Join(dir, ".python-version"))
}

func TestPadPEP440(t *testing.T) {
	tests := []struct {
		spec string
		want string
	}{
		{spec: ">3.12", want: ">3.12.0"},
		{spec: "<=3.12", want: "<=3.12.0"},
		{spec: "==3.12", want: "==3.12.0"},
		{spec: "!=3.12", want: "!=3.12.0"},
		{spec: ">=3.10,<3.14", want: ">=3.10,<3.14"},
		{spec: "~=3.11", want: "~=3.11"},
		{spec: "==3.12.*", want: "==3.12.*"},
		{spec: ">3.12.1", want: ">3.12.1"},
		{spec: ">= 3.9, != 3.10, < 4", want: ">= 3.9, !=3.10.0, < 4"},
	}
	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			assert.Equal(t, padPEP440(tc.spec), tc.want)
		})
	}
}

// TestRequiresPython_ResolvesAsPEP440 checks the specifiers npm-style ranges
// read differently resolve as pip would.
func TestRequiresPython_ResolvesAsPEP440(t *testing.T) {
	tests := []struct {
		requires string
		want     string
	}{
		{requires: ">3.12", want: "3.14.1"},
		{requires: ">3.12,<3.13", want: "3.12.10"},
		{requires: "<=3.12", want: "3.12.0"},
		{requires: ">=3.13,<3.14", want: "3.13.2"},
	}
	for _, tc := range tests {
		t.Run(tc.requires, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pyproject.toml")
			assert.NilError(t, os.WriteFile(path, []byte("[project]\nrequires-python = \""+tc.requires+"\"\n"), 0600))
			raw, err := FromFile(path)
			assert.NilError(t, err)
			spec, err := Parse(raw, false, false)
			assert.NilError(t, err)
			got, err := spec.Resolve(append(versions, "3.12.0"))
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}
