//go:build linux

// Package homebrew provides metadata-only, non-authorizing local inspection.
package homebrew

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type Identity struct {
	Device uint64
	Inode  uint64
}
type Profile struct {
	Package         string
	Config          string
	PackageIdentity Identity
}
type Result struct {
	Code          string
	RuntimeConfig string
	SystemdTrust  string
}

func (r Result) String() string {
	return fmt.Sprintf("%s runtime/config=%s systemd=%s", r.Code, r.RuntimeConfig, r.SystemdTrust)
}
func result(code string) Result { return Result{code, "NOT VERIFIED", "NOT VERIFIED"} }
func metadata(f os.FileInfo) (Identity, uint32, bool) {
	if f == nil {
		return Identity{}, 0, false
	}
	st, ok := f.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return Identity{}, 0, false
	}
	return Identity{uint64(st.Dev), st.Ino}, st.Uid, true
}
func device(f os.FileInfo) uint64 { id, _, _ := metadata(f); return id.Device }
func inode(f os.FileInfo) uint64  { id, _, _ := metadata(f); return id.Inode }

// Inspect is a metadata-only snapshot, not proof of provenance or mutation eligibility.
func Inspect(p Profile) Result { return inspectWith(p, os.Lstat) }
func inspectWith(p Profile, lstat func(string) (os.FileInfo, error)) Result {
	if p.PackageIdentity.Device == 0 || p.PackageIdentity.Inode == 0 {
		return result("invalid_profile")
	}
	for _, target := range []struct{ path, kind string }{{p.Package, "package"}, {p.Config, "config"}} {
		if !filepath.IsAbs(target.path) || filepath.Clean(target.path) != target.path || target.path == "/" {
			return result("invalid_profile")
		}
		path := target.path
		for {
			f, err := lstat(path)
			if err != nil {
				if os.IsNotExist(err) {
					return result("missing_" + target.kind)
				}
				return result("unavailable_" + target.kind)
			}
			_, uid, ok := metadata(f)
			if !ok || (uid != uint32(os.Getuid()) && uid != 0) || f.Mode().Perm()&0022 != 0 || f.Mode()&os.ModeSymlink != 0 {
				return result("unsafe_" + target.kind)
			}
			if path == target.path {
				if !f.Mode().IsRegular() {
					return result("unsafe_" + target.kind)
				}
			} else if !f.IsDir() {
				return result("unsafe_" + target.kind)
			}
			if path == "/" {
				break
			}
			path = filepath.Dir(path)
		}
	}
	f, err := lstat(p.Package)
	if err != nil {
		return result("unavailable_package")
	}
	id, _, ok := metadata(f)
	if !ok || !f.Mode().IsRegular() {
		return result("unsafe_package")
	}
	if id != p.PackageIdentity {
		return result("stale_package")
	}
	return result("inspection_only")
}
