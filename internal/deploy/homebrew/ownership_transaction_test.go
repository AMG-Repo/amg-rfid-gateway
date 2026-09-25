//go:build linux

package homebrew

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The adapter is deliberately test-only: the transaction never changes kernel ownership.
type ownershipFixtureAdapter struct {
	root    string
	modeled map[string]ownershipObject
}

func ownershipFixture(t *testing.T) (ownershipFixtureTransaction, *ownershipFixtureAdapter) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(root, "tree")
	if err := os.Mkdir(tree, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "file"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tree, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	a := &ownershipFixtureAdapter{root: tree, modeled: make(map[string]ownershipObject)}
	var objects []ownershipEntry
	for _, item := range []struct {
		path, kind    string
		before, after ownershipState
	}{
		{".", "directory", ownershipState{1000, 1000, 0700}, ownershipState{2000, 2000, 0700}},
		{"empty", "directory", ownershipState{1000, 1000, 0750}, ownershipState{2000, 2000, 0700}},
		{"file", "file", ownershipState{1000, 1000, 0640}, ownershipState{2000, 2000, 0600}},
	} {
		info, err := os.Lstat(filepath.Join(tree, item.path))
		if err != nil {
			t.Fatal(err)
		}
		id, _, _ := metadata(info)
		entry := ownershipEntry{Path: item.path, Kind: item.kind, Identity: id, Before: item.before, After: item.after}
		objects = append(objects, entry)
		a.modeled[item.path] = ownershipObject{Kind: item.kind, Identity: id, State: item.before}
	}
	return ownershipFixtureTransaction{root: tree, journal: filepath.Join(root, "journal"), entries: objects, adapter: a}, a
}

func (a *ownershipFixtureAdapter) observe(path string) (ownershipObject, error) {
	obj, ok := a.modeled[path]
	if !ok {
		return ownershipObject{}, errOwnershipFixture
	}
	return obj, nil
}
func (a *ownershipFixtureAdapter) mutate(path string, before, after ownershipState) error {
	obj, err := a.observe(path)
	if err != nil || obj.State != before {
		return errOwnershipFixture
	}
	obj.State = after
	a.modeled[path] = obj
	return nil
}

func TestOwnershipFixtureRoundTrip(t *testing.T) {
	tx, a := ownershipFixture(t)
	if err := tx.run(false, nil); err != nil {
		t.Fatal(err)
	}
	for _, entry := range tx.entries {
		if got := a.modeled[entry.Path].State; got != entry.After {
			t.Fatalf("%s: %v", entry.Path, got)
		}
	}
	if err := tx.run(false, nil); err != nil {
		t.Fatal("apply retry:", err)
	}
	if err := tx.run(true, nil); err != nil {
		t.Fatal("inverse:", err)
	}
	if err := tx.run(true, nil); err != nil {
		t.Fatal("inverse retry:", err)
	}
	for _, entry := range tx.entries {
		if got := a.modeled[entry.Path].State; got != entry.Before {
			t.Fatalf("%s: %v", entry.Path, got)
		}
	}
}

func TestOwnershipFixtureInterruptions(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, boundary := range []string{"intent", "before", "mutation", "progress", "done"} {
			for index := 0; index < 3; index++ {
				if (boundary == "intent" || boundary == "done") && index != 0 {
					continue
				}
				t.Run(boundary+string(rune('0'+index))+map[bool]string{true: "inverse", false: "apply"}[reverse], func(t *testing.T) {
					tx, _ := ownershipFixture(t)
					if reverse {
						if err := tx.run(false, nil); err != nil {
							t.Fatal(err)
						}
					}
					err := tx.run(reverse, func(phase string, n int) error {
						if phase == boundary && n == index {
							return errors.New("interrupted")
						}
						return nil
					})
					if err == nil {
						t.Fatal("missing interruption")
					}
					if err := tx.run(reverse, nil); err != nil {
						t.Fatal("recovery:", err)
					}
					if err := tx.run(reverse, nil); err != nil {
						t.Fatal("idempotence:", err)
					}
				})
			}
		}
	}
}

func TestOwnershipFixtureRejectsWidening(t *testing.T) {
	tx, a := ownershipFixture(t)
	tx.entries[2].After.Mode = 0666
	if err := tx.run(false, nil); err == nil {
		t.Fatal("accepted permission widening")
	}
	if a.modeled["."].State != tx.entries[0].Before {
		t.Fatal("mutated tree before rejecting widening")
	}
}

func TestOwnershipFixtureRejectsLaterUnrecordedMutation(t *testing.T) {
	tx, a := ownershipFixture(t)
	if err := tx.run(false, func(phase string, index int) error {
		if phase == "mutation" && index == 0 {
			return os.ErrClosed
		}
		return nil
	}); err != os.ErrClosed {
		t.Fatal(err)
	}
	obj := a.modeled["file"]
	obj.State = tx.entries[2].After
	a.modeled["file"] = obj
	if err := tx.run(false, nil); err == nil {
		t.Fatal("accepted later unrecorded mutation")
	}
}

func TestOwnershipFixtureRejectsNonPrefixProgress(t *testing.T) {
	tx, _ := ownershipFixture(t)
	r := ownershipFixtureRecord{Version: 1, Root: tx.entries[0].Identity, Entries: tx.entries, Direction: "apply", Phase: "pending", Progress: []bool{false, true, false}}
	if err := tx.record(r); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tx.read(); err == nil {
		t.Fatal("accepted non-prefix progress")
	}
}

func TestOwnershipFixtureRefusals(t *testing.T) {
	for _, scenario := range []string{"replacement", "missing", "extra", "symlink", "partial", "unknown", "inverse-before-complete", "inverse-intervening", "tampered-record", "changed-plan", "hardlink", "journal-symlink", "mode-drift", "leftover-temp", "unsupported-kind"} {
		t.Run(scenario, func(t *testing.T) {
			tx, a := ownershipFixture(t)
			switch scenario {
			case "replacement":
				if err := os.Rename(filepath.Join(tx.root, "file"), filepath.Join(filepath.Dir(tx.root), "old-file")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(tx.root, "file"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(filepath.Join(tx.root, "empty")); err != nil {
					t.Fatal(err)
				}
			case "extra":
				if err := os.WriteFile(filepath.Join(tx.root, "extra"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("file", filepath.Join(tx.root, "link")); err != nil {
					t.Fatal(err)
				}
			case "partial":
				a.modeled["file"] = ownershipObject{Kind: "file", Identity: tx.entries[2].Identity, State: tx.entries[2].After}
			case "unknown":
				delete(a.modeled, "file")
			case "inverse-before-complete":
				if err := tx.run(false, func(phase string, n int) error {
					if phase == "mutation" && n == 0 {
						return os.ErrClosed
					}
					return nil
				}); err != os.ErrClosed {
					t.Fatal(err)
				}
			case "inverse-intervening":
				if err := tx.run(false, nil); err != nil {
					t.Fatal(err)
				}
				obj := a.modeled["file"]
				obj.State.Mode = 0644
				a.modeled["file"] = obj
			case "tampered-record":
				if err := tx.run(false, nil); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(tx.journal, []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-plan":
				if err := tx.run(false, nil); err != nil {
					t.Fatal(err)
				}
				tx.entries[2].After.Mode = 0644
			case "hardlink":
				if err := os.Link(filepath.Join(tx.root, "file"), filepath.Join(filepath.Dir(tx.root), "alias")); err != nil {
					t.Fatal(err)
				}
			case "journal-symlink":
				if err := os.Symlink(filepath.Join(tx.root, "file"), tx.journal); err != nil {
					t.Fatal(err)
				}
			case "mode-drift":
				obj := a.modeled["file"]
				obj.State.Mode = 0666
				a.modeled["file"] = obj
			case "leftover-temp":
				if err := os.WriteFile(tx.journal+".new", []byte("incomplete"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unsupported-kind":
				if err := os.Remove(filepath.Join(tx.root, "file")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(tx.root, "file"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			reverse := scenario == "inverse-before-complete" || scenario == "inverse-intervening"
			if err := tx.run(reverse, nil); err == nil {
				t.Fatal("accepted unsafe state")
			}
		})
	}
}
