//go:build linux

package homebrew

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspect(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "package")
	cfg := filepath.Join(root, "config-secret")
	if err := os.WriteFile(pkg, []byte("binary"), 0600); err != nil {
		t.Fatal(err)
	}
	secret := "private-password-value"
	if err := os.WriteFile(cfg, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(pkg)
	if err != nil {
		t.Fatal(err)
	}
	p := Profile{Package: pkg, Config: cfg, PackageIdentity: Identity{Device: device(info), Inode: inode(info)}}
	for _, tc := range []struct {
		name   string
		mutate func()
		want   string
	}{
		{"ordinary", func() {}, "inspection_only"},
		{"stale", func() { p.PackageIdentity.Inode++ }, "stale_package"},
		{"absent", func() { p.Config = filepath.Join(root, "absent") }, "missing_config"},
		{"symlink", func() {
			link := filepath.Join(root, "link")
			if err := os.Symlink(cfg, link); err != nil {
				t.Fatal(err)
			}
			p.Config = link
		}, "unsafe_config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saved := p
			defer func() { p = saved }()
			tc.mutate()
			result := inspectWith(p, trustedFixtureStat)
			if result.Code != tc.want {
				t.Fatalf("code %q want %q", result.Code, tc.want)
			}
			if strings.Contains(result.String(), secret) || strings.Contains(result.String(), root) {
				t.Fatal("disclosed secret or path")
			}
		})
	}
}
