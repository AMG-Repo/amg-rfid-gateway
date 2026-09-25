//go:build linux

package homebrew

import (
	"os"
	"path/filepath"
	"testing"
)

func TestObserveRuntimeEvidenceRejectsWritableIntermediateAncestor(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(cwd, "runtime-ancestor-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	ancestor := filepath.Join(root, "writable-parent")
	if err := os.Mkdir(ancestor, 0700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(ancestor, "runtime")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	id, owner, ok := metadata(info)
	if !ok {
		t.Fatal("metadata unavailable")
	}
	candidate := RuntimeCandidate{Directory: dir, Socket: filepath.Join(dir, "gateway.sock"), Expected: Condition{Identity: id, Owner: owner, Mode: 0700}}
	if got := ObserveRuntimeEvidence(candidate); got.Code != "runtime_metadata_observed" {
		t.Fatalf("baseline did not reach runtime directory %q: %+v", dir, got)
	}
	if err := os.Chmod(ancestor, 0770); err != nil {
		t.Fatal(err)
	}
	parentInfo, err := os.Lstat(ancestor)
	if err != nil {
		t.Fatal(err)
	}
	if parentInfo.Mode().Perm() != 0770 {
		t.Fatalf("intermediate ancestor %q mode = %04o, want 0770", ancestor, parentInfo.Mode().Perm())
	}
	if got := ObserveRuntimeEvidence(candidate); got.Code != "unsafe_runtime_path" {
		t.Fatalf("writable intermediate ancestor %q (mode %04o) on path to %q: %+v", ancestor, parentInfo.Mode().Perm(), dir, got)
	}
}

func TestObserveRuntimeEvidence(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(cwd, "runtime-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "runtime")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	id, owner, ok := metadata(info)
	if !ok {
		t.Fatal("metadata unavailable")
	}
	candidate := RuntimeCandidate{Directory: dir, Socket: filepath.Join(dir, "gateway.sock"), Expected: Condition{Identity: id, Owner: owner, Mode: 0700}}
	if got := ObserveRuntimeEvidence(candidate); got.Code != "runtime_metadata_observed" || got.DirectoryIdentity != id {
		t.Fatalf("protected metadata: %+v", got)
	}
	if err := os.Symlink("elsewhere", candidate.Socket); err != nil {
		t.Fatal(err)
	}
	if got := ObserveRuntimeEvidence(candidate); got.Code == "runtime_metadata_observed" {
		t.Fatal("existing socket candidate accepted")
	}
	if err := os.Remove(candidate.Socket); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(candidate.Socket, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if got := ObserveRuntimeEvidence(candidate); got.Code != "socket_candidate_unavailable" {
		t.Fatalf("occupied regular-file socket name: %+v", got)
	}
	if err := os.Remove(candidate.Socket); err != nil {
		t.Fatal(err)
	}
	missing := candidate
	missing.Directory = filepath.Join(root, "missing")
	missing.Socket = filepath.Join(missing.Directory, "gateway.sock")
	if got := ObserveRuntimeEvidence(missing); got.Code == "runtime_metadata_observed" {
		t.Fatal("missing accepted")
	}
	if err := os.Symlink(dir, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	link := candidate
	link.Directory = filepath.Join(root, "link")
	link.Socket = filepath.Join(link.Directory, "gateway.sock")
	if got := ObserveRuntimeEvidence(link); got.Code == "runtime_metadata_observed" {
		t.Fatal("symlink accepted")
	}
	replacement := filepath.Join(root, "replacement")
	if err := os.Rename(dir, replacement); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if got := ObserveRuntimeEvidence(candidate); got.Code == "runtime_metadata_observed" {
		t.Fatal("replacement accepted")
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	fresh, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	freshID, _, _ := metadata(fresh)
	unsafe := candidate
	unsafe.Expected.Identity = freshID
	// Keep the expected metadata valid so the refusal comes from the opened directory.
	unsafe.Expected.Mode = 0700
	if got := ObserveRuntimeEvidence(unsafe); got.Code != "unsafe_runtime_path" {
		t.Fatalf("unsafe on-disk mode: %+v", got)
	}
}
