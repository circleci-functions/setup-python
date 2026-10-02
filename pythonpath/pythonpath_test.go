package pythonpath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

// fakeInstall writes a Unix Python install whose python reports 3.13.1 and
// names userScripts as pip's user scripts directory.
func fakeInstall(t *testing.T, userScripts string) string {
	t.Helper()
	skip.If(t, runtime.GOOS == "windows", "the fake install's python is a shell script")
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	assert.NilError(t, os.MkdirAll(bin, 0750))
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo 'Python 3.13.1' ;;\n-c) echo '" + userScripts + "' ;;\nesac\n"
	assert.NilError(t, os.WriteFile(filepath.Join(bin, "python3.13"), []byte(script), 0700)) //nolint:gosec // must be executable
	for _, name := range []string{"python", "python3"} {
		assert.NilError(t, os.Symlink("python3.13", filepath.Join(bin, name)))
	}
	for _, name := range []string{"pip", "pip3", "pip3.13"} {
		assert.NilError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0700)) //nolint:gosec // must be executable
	}
	return root
}

func TestLink_DiscoversDirOnPath(t *testing.T) {
	root := fakeInstall(t, "")
	onPath := t.TempDir()

	res, err := Link(Options{Root: root, Version: "3.13.1", Path: onPath})
	assert.NilError(t, err)
	assert.DeepEqual(t, res, Result{Dir: onPath, Root: root})
	for _, name := range []string{"python", "python3", "python3.13", "pip", "pip3", "pip3.13"} {
		target, err := os.Readlink(filepath.Join(onPath, name))
		assert.NilError(t, err, name)
		assert.Equal(t, target, filepath.Join(root, "bin", name))
	}
}

func TestLink_BinDir(t *testing.T) {
	root := fakeInstall(t, "")
	binDir := t.TempDir()

	res, err := Link(Options{Root: root, Version: "3.13.1", BinDir: binDir})
	assert.NilError(t, err)
	assert.Equal(t, res.Dir, binDir)
	_, err = os.Stat(filepath.Join(binDir, "pip"))
	assert.NilError(t, err)
}

// TestLink_TakesOverAPipAhead covers a preinstalled Python whose pip sits in a
// directory of its own ahead of the one python is linked into, as the Scripts
// directory of a Windows install does.
func TestLink_TakesOverAPipAhead(t *testing.T) {
	root := fakeInstall(t, "")
	scripts, pyDir := t.TempDir(), t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(scripts, "pip"), []byte("#!/bin/sh\n"), 0700))                        //nolint:gosec // must be executable
	assert.NilError(t, os.WriteFile(filepath.Join(pyDir, "python"), []byte("#!/bin/sh\necho 'Python 3.12.0'\n"), 0700)) //nolint:gosec // must be executable
	path := scripts + string(os.PathListSeparator) + pyDir

	res, err := Link(Options{Root: root, Version: "3.13.1", Path: path})
	assert.NilError(t, err)
	assert.Equal(t, res.Dir, pyDir)
	assert.DeepEqual(t, res.PipDirs, []string{scripts})
	target, err := os.Readlink(filepath.Join(scripts, "pip"))
	assert.NilError(t, err)
	assert.Equal(t, target, filepath.Join(root, "bin", "pip"))
	assert.Equal(t, shadowed(path, pyDir, "pip"), scripts)
	assert.Equal(t, shadowed(path, pyDir, "pip3"), "")
}

func TestLink_InvalidVersion(t *testing.T) {
	_, err := Link(Options{Root: t.TempDir(), Version: "3"})
	assert.ErrorContains(t, err, `invalid Python version "3"`)
}

func TestVerify(t *testing.T) {
	root := fakeInstall(t, "")
	other := fakeInstall(t, "")
	broken := fakeInstall(t, "")
	assert.NilError(t, os.WriteFile(filepath.Join(broken, "bin", "python3.13"), []byte("#!/bin/sh\necho 'GLIBC_2.17 not found' >&2\nexit 1\n"), 0700)) //nolint:gosec // must be executable

	link := filepath.Join(t.TempDir(), "python")
	assert.NilError(t, os.Symlink(filepath.Join(root, "bin", "python"), link))

	tests := []struct {
		name    string
		from    string
		version string
		bin     string
		wantErr string
	}{
		{name: "this install", from: root, version: "3.13.1", bin: link},
		{name: "another version", from: root, version: "3.12.1", bin: link, wantErr: `reports "Python 3.13.1" rather than Python 3.12.1`},
		{name: "a lookalike", from: other, version: "3.13.1", bin: link, wantErr: "is not " + filepath.Join(other, "bin", "python")},
		{name: "does not run", from: broken, version: "3.13.1", bin: filepath.Join(broken, "bin", "python"), wantErr: "GLIBC_2.17 not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := verify(filepath.Join(tc.from, "bin"), tc.version)(tc.bin)
			if tc.wantErr == "" {
				assert.NilError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestUserInstalls(t *testing.T) {
	linked := t.TempDir()
	root := fakeInstall(t, linked)

	got, err := UserInstalls(root, linked)
	assert.NilError(t, err)
	assert.Equal(t, got, linked)
	conf, err := os.ReadFile(filepath.Join(root, "pip.conf"))
	assert.NilError(t, err)
	assert.Equal(t, string(conf), "[install]\nuser = true\n")
}

func TestUserInstalls_ElsewhereRemovesTheSetting(t *testing.T) {
	userScripts := t.TempDir()
	root := fakeInstall(t, userScripts)
	assert.NilError(t, os.WriteFile(filepath.Join(root, "pip.conf"), []byte("[install]\nuser = true\n"), 0600))
	linked := t.TempDir()

	got, err := UserInstalls(root, linked)
	assert.ErrorContains(t, err, "pip's user scripts directory "+userScripts+" is not "+linked)
	assert.Equal(t, got, userScripts)
	_, err = os.Stat(filepath.Join(root, "pip.conf"))
	assert.Assert(t, os.IsNotExist(err), "the setting from an earlier run was left in place")
}

func TestShadowed(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "the fake executables are shell scripts")
	ahead, linked := t.TempDir(), t.TempDir()
	for _, dir := range []string{ahead, linked} {
		assert.NilError(t, os.WriteFile(filepath.Join(dir, "pip"), []byte("#!/bin/sh\n"), 0700)) //nolint:gosec // must be executable
	}

	t.Setenv("PATH", linked+string(os.PathListSeparator)+ahead)
	assert.Equal(t, Shadowed(linked, "pip"), "")
	assert.Equal(t, Shadowed(linked, "nothing-by-this-name"), "")

	t.Setenv("PATH", ahead+string(os.PathListSeparator)+linked)
	assert.Equal(t, Shadowed(linked, "pip"), ahead)
}

func TestWritePipShims(t *testing.T) {
	dir := t.TempDir()
	assert.NilError(t, writePipShims(dir, `C:\py\python.exe`, "pip3"))

	bat, err := os.ReadFile(filepath.Join(dir, "pip3.bat"))
	assert.NilError(t, err)
	assert.Equal(t, string(bat), "@echo off\r\n\"C:\\py\\python.exe\" -m pip %*\r\n")
	sh, err := os.ReadFile(filepath.Join(dir, "pip3"))
	assert.NilError(t, err)
	assert.Equal(t, string(sh), "#!/bin/sh\nexec \""+filepath.ToSlash(`C:\py\python.exe`)+"\" -m pip \"$@\"\n")
}
