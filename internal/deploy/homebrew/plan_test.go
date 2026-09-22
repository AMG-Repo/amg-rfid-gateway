//go:build linux

package homebrew

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductionReaderOnTrustedSourceFile(t *testing.T) {
	_, path, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("missing caller")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := readPackage(path, Identity{device(f), inode(f)})
	if err != nil || !strings.Contains(string(data), "TestProductionReaderOnTrustedSourceFile") {
		t.Fatalf("real reader failed: %v", err)
	}
}
func TestContentObservation(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "gateway")
	cfg := filepath.Join(root, "config-secret")
	if err := os.WriteFile(pkg, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("private-value"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Lstat(pkg)
	if err != nil {
		t.Fatal(err)
	}
	p := Profile{Package: pkg, Config: cfg, PackageIdentity: Identity{device(f), inode(f)}}
	sum := sha256.Sum256([]byte("synthetic"))
	ev := evidence{archive: "synthetic-test-only", member: hex.EncodeToString(sum[:])}
	read := func(string) ([]byte, error) { return []byte("synthetic"), nil }
	got := observeWithEvidence(p, "v0.6.5", "linux-amd64", ev, trustedFixtureStat, read)
	again := observeWithEvidence(p, "v0.6.5", "linux-amd64", ev, trustedFixtureStat, read)
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(again)
	if string(a) != string(b) || got.Code != "content_equal" || got.ApplyEligible || got.Inventory.Package.Identity != p.PackageIdentity || len(got.Blockers) < 4 {
		t.Fatalf("invalid observation: %s", a)
	}
	if strings.Contains(string(a), root) || strings.Contains(string(a), "private-value") {
		t.Fatal("disclosure")
	}
	if r := observeWith(p, "v0.6.5", "linux-amd64", trustedFixtureStat, read); r.Code != "content_mismatch" {
		t.Fatalf("production evidence bypass: %+v", r)
	}
	for _, tc := range []struct{ version, arch string }{{"v0.6.4", "linux-amd64"}, {"v0.6.5", "linux-other"}, {"", ""}} {
		if r := observeWith(p, tc.version, tc.arch, trustedFixtureStat, nil); r.Code != "unsupported_evidence" {
			t.Fatalf("%+v", r)
		}
	}
	if r := observeWith(p, "v0.6.5", "linux-amd64", trustedFixtureStat, nil); r.Code != "unavailable_package" {
		t.Fatalf("%+v", r)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(pkg, link); err != nil {
		t.Fatal(err)
	}
	p.Package = link
	if r := observeWith(p, "v0.6.5", "linux-amd64", trustedFixtureStat, nil); r.Code != "unsafe_package" {
		t.Fatalf("%+v", r)
	}
	p.Package = pkg
	p.PackageIdentity.Inode++
	if r := observeWith(p, "v0.6.5", "linux-amd64", trustedFixtureStat, nil); r.Code != "stale_package" {
		t.Fatalf("%+v", r)
	}
}
