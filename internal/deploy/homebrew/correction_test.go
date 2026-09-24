//go:build linux

package homebrew

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type unsupportedInfo struct{ os.FileInfo }

func (unsupportedInfo) Sys() any { return nil }

type modeInfo struct {
	os.FileInfo
	mode os.FileMode
}

func (f modeInfo) Mode() os.FileMode { return f.mode }

func TestAncestorAndIdentityRefusal(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "package")
	cfg := filepath.Join(root, "config")
	for _, path := range []string{pkg, cfg} {
		if err := os.WriteFile(path, []byte("secret-value"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Lstat(pkg)
	if err != nil {
		t.Fatal(err)
	}
	p := Profile{Package: pkg, Config: cfg, PackageIdentity: Identity{device(info), inode(info)}}
	base := trustedFixtureStat
	for _, tc := range []struct {
		name     string
		alter    func(string, os.FileInfo) os.FileInfo
		identity Identity
		want     string
	}{
		{"ordinary", func(_ string, f os.FileInfo) os.FileInfo { return f }, p.PackageIdentity, "inspection_only"},
		{"sticky root ancestor", func(path string, f os.FileInfo) os.FileInfo {
			if path == "/tmp" {
				return modeInfo{f, f.Mode() | os.ModeSticky | 0002}
			}
			return f
		}, p.PackageIdentity, "unsafe_package"},
		{"group writable ancestor", func(path string, f os.FileInfo) os.FileInfo {
			if path == root {
				return modeInfo{f, f.Mode() | 0020}
			}
			return f
		}, p.PackageIdentity, "unsafe_package"},
		{"unsupported metadata", func(path string, f os.FileInfo) os.FileInfo {
			if path == pkg {
				return unsupportedInfo{f}
			}
			return f
		}, p.PackageIdentity, "unsafe_package"},
		{"zero device", func(_ string, f os.FileInfo) os.FileInfo { return f }, Identity{0, p.PackageIdentity.Inode}, "invalid_profile"},
		{"zero inode", func(_ string, f os.FileInfo) os.FileInfo { return f }, Identity{p.PackageIdentity.Device, 0}, "invalid_profile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p.PackageIdentity = tc.identity
			got := inspectWith(p, func(path string) (os.FileInfo, error) {
				f, e := base(path)
				if e != nil {
					return nil, e
				}
				return tc.alter(path, f), nil
			})
			if got.Code != tc.want {
				t.Fatalf("got %s want %s", got.Code, tc.want)
			}
			if strings.Contains(got.String(), root) || strings.Contains(got.String(), "secret-value") {
				t.Fatal("disclosure")
			}
		})
	}
}

// trustedFixtureStat models a private, non-writable fixture root in tests only.
func trustedFixtureStat(path string) (os.FileInfo, error) {
	f, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if path == "/tmp" && f.IsDir() {
		return modeInfo{f, f.Mode() &^ 0022}, nil
	}
	return f, nil
}

func TestSymlinkedAncestor(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(real, "package")
	cfg := filepath.Join(real, "config")
	for _, p := range []string{pkg, cfg} {
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Lstat(pkg)
	if err != nil {
		t.Fatal(err)
	}
	p := Profile{Package: filepath.Join(link, "package"), Config: cfg, PackageIdentity: Identity{device(f), inode(f)}}
	if got := inspectWith(p, trustedFixtureStat); got.Code != "unsafe_package" {
		t.Fatalf("got %s", got.Code)
	}
}
