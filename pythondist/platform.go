// Package pythondist finds, downloads, verifies and unpacks the relocatable
// CPython builds published by python-build-standalone, as listed in uv's
// download metadata.
package pythondist

import (
	"debug/elf"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
)

// Platform is a build target, in uv's names.
type Platform struct {
	// OS is linux, darwin or windows.
	OS string
	// Arch is the architecture family, e.g. x86_64 or aarch64.
	Arch string
	// Libc is gnu, musl or gnueabihf on Linux, and none elsewhere.
	Libc string
}

var archFamily = map[string]string{
	"amd64":   "x86_64",
	"arm64":   "aarch64",
	"386":     "i686",
	"arm":     "armv7",
	"ppc64le": "powerpc64le",
	"s390x":   "s390x",
	"riscv64": "riscv64",
}

// Host returns the platform setup-python is running on.
func Host() (Platform, error) {
	return platformFor(runtime.GOOS, runtime.GOARCH, isMusl())
}

func platformFor(goos, goarch string, musl bool) (Platform, error) {
	arch, ok := archFamily[goarch]
	if !ok {
		return Platform{}, fmt.Errorf("unsupported architecture %s", goarch)
	}
	switch goos {
	case "darwin", "windows":
		return Platform{OS: goos, Arch: arch, Libc: "none"}, nil
	case "linux":
		libc := "gnu"
		switch {
		case musl:
			libc = "musl"
		case goarch == "arm":
			libc = "gnueabihf"
		}
		return Platform{OS: goos, Arch: arch, Libc: libc}, nil
	}
	return Platform{}, fmt.Errorf("unsupported operating system %s", goos)
}

// isMusl reports whether this Linux uses musl rather than glibc, as Alpine
// does, by the dynamic loader /bin/sh asks for. The loader merely existing is
// not enough: a glibc distro can install musl alongside.
func isMusl() bool {
	if interp, ok := interpreter("/bin/sh"); ok {
		return muslInterpreter(interp)
	}
	matches, _ := filepath.Glob("/lib/ld-musl-*.so.1")
	return len(matches) > 0
}

// interpreter returns the dynamic loader the ELF executable at path names.
func interpreter(path string) (string, bool) {
	f, err := elf.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_INTERP {
			continue
		}
		data, err := io.ReadAll(prog.Open())
		if err != nil {
			return "", false
		}
		return strings.TrimRight(string(data), "\x00"), true
	}
	return "", false
}

func muslInterpreter(interp string) bool {
	return strings.Contains(filepath.Base(interp), "musl")
}

func (p Platform) String() string {
	if p.Libc == "none" {
		return p.OS + "-" + p.Arch
	}
	return p.OS + "-" + p.Arch + "-" + p.Libc
}
