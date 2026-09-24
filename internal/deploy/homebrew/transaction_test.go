//go:build linux

package homebrew

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestFixtureTransactionUsesCurrentCheckout(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tx := fixtureTransaction(t)
	if filepath.Dir(filepath.Dir(tx.root)) != cwd {
		t.Fatalf("fixture parent = %q, want %q", filepath.Dir(filepath.Dir(tx.root)), cwd)
	}
}

func fixtureTransaction(t *testing.T) dataTransaction {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(cwd, "transaction-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	data, journal := filepath.Join(root, "data"), filepath.Join(root, "journal")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(data, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(journal, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Lstat(data)
	if err != nil {
		t.Fatal(err)
	}
	id, owner, _ := metadata(f)
	return dataTransaction{data: data, root: journal, before: Condition{id, owner, 0775}}
}

func TestTransactionSyncFailures(t *testing.T) {
	for _, site := range []string{"root", "pending-file", "pending-directory", "target", "verified-target", "parent", "completion-file", "completion-directory"} {
		t.Run(site, func(t *testing.T) {
			tx := fixtureTransaction(t)
			calls := 0
			tx.fsync = func(fd int) error {
				calls++
				index := map[string]int{"root": 1, "pending-file": 2, "pending-directory": 3, "target": 4, "verified-target": 5, "parent": 6, "completion-file": 7, "completion-directory": 8}[site]
				if calls == index {
					return syscall.EINVAL
				}
				return syscall.Fsync(fd)
			}
			if err := tx.run(false, nil); err == nil {
				t.Fatal("unsupported sync accepted")
			}
			tx.fsync = nil
			if site == "root" {
				if _, err := os.Stat(filepath.Join(tx.root, "record")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("root failure mutated journal: %v", err)
				}
			}
			if err := tx.run(false, nil); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if err := tx.run(false, nil); err != nil {
				t.Fatalf("idempotence: %v", err)
			}
		})
	}
}

func TestTransactionRecoverySyncsTempBeforePromotion(t *testing.T) {
	tx := fixtureTransaction(t)
	fd := txRoot(t, tx)
	rec := transactionRecord{1, tx.before.Identity, tx.before.Owner, "apply", "pending"}
	if err := tx.syncRecord(fd, rec); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(tx.root, "record"), filepath.Join(tx.root, "record.new")); err != nil {
		t.Fatal(err)
	}
	tx.fsync = func(fd int) error {
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) == nil && st.Mode&syscall.S_IFMT == syscall.S_IFREG && st.Size > 0 {
			return syscall.EINVAL
		}
		return syscall.Fsync(fd)
	}
	if err := tx.run(false, nil); err == nil {
		t.Fatal("promoted unsynced temp")
	}
	if _, err := os.Stat(filepath.Join(tx.root, "record")); !os.IsNotExist(err) {
		t.Fatalf("record changed: %v", err)
	}
	tx.fsync = nil
	if err := tx.run(false, nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.run(false, nil); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionCompletionTempRequiresExactPostState(t *testing.T) {
	tx := fixtureTransaction(t)
	tx.fsync = func(fd int) error {
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) == nil && st.Mode&syscall.S_IFMT == syscall.S_IFREG && st.Size > 0 {
			rec, exists, _ := readRecord(txRoot(t, tx))
			if exists && rec.Phase == "pending" {
				return syscall.EINVAL
			}
		}
		return syscall.Fsync(fd)
	}
	if err := tx.run(false, nil); err == nil {
		t.Fatal("completion sync failure accepted")
	}
	tx.fsync = nil
	if err := os.Chmod(tx.data, 0775); err != nil {
		t.Fatal(err)
	}
	if err := tx.run(false, nil); err == nil {
		t.Fatal("uncertain post-state accepted")
	}
	if err := os.Chmod(tx.data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := tx.run(false, nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.run(false, nil); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionFailures(t *testing.T) {
	for _, boundary := range []string{"before_pending", "after_pending", "before_mutation", "after_mutation", "after_target_sync", "before_completion", "after_completion"} {
		t.Run(boundary, func(t *testing.T) {
			tx := fixtureTransaction(t)
			injected := os.ErrClosed
			err := tx.run(false, func(s string) error {
				if s == boundary {
					return injected
				}
				return nil
			})
			if err != injected {
				t.Fatalf("boundary failure: %v", err)
			}
			if err := tx.run(false, nil); err != nil {
				t.Fatal(err)
			}
			if err := tx.run(false, nil); err != nil {
				t.Fatal(err)
			}
			if err := tx.run(true, nil); err != nil {
				t.Fatal(err)
			}
			if err := tx.run(true, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestTransactionRefusesReplacementAndCorruption(t *testing.T) {
	tx := fixtureTransaction(t)
	if err := tx.run(false, func(s string) error {
		if s == "after_pending" {
			return os.ErrClosed
		}
		return nil
	}); err != os.ErrClosed {
		t.Fatal(err)
	}
	old := tx.data + "-old"
	if err := os.Rename(tx.data, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(tx.data, 0775); err != nil {
		t.Fatal(err)
	}
	if err := tx.run(false, nil); err == nil {
		t.Fatal("replacement accepted")
	}
	if err := os.WriteFile(filepath.Join(tx.root, "record"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := tx.run(false, nil); err == nil {
		t.Fatal("corruption accepted")
	}
}
func TestTransactionRollbackRefusesReplacementAfterCompletedApply(t *testing.T) {
	tx := fixtureTransaction(t)
	if err := tx.run(false, nil); err != nil {
		t.Fatal(err)
	}
	original := tx.data + "-original"
	if err := os.Rename(tx.data, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(tx.data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := tx.run(true, nil); err == nil {
		t.Fatal("rollback accepted replacement after completed apply")
	}
	info, err := os.Lstat(tx.data)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatalf("replacement changed by rollback: %v", info.Mode())
	}
}

func TestTransactionRefusesUncompletedRollback(t *testing.T) {
	tx := fixtureTransaction(t)
	if err := tx.run(true, nil); err == nil {
		t.Fatal("rollback without completed apply")
	}
	f, _ := os.Lstat(tx.data)
	if f.Mode().Perm() != 0775 {
		t.Fatal("changed before authorization")
	}
}

func TestTransactionRecoversTempRecord(t *testing.T) {
	tx := fixtureTransaction(t)
	rec := transactionRecord{1, tx.before.Identity, tx.before.Owner, "apply", "pending"}
	if err := tx.syncRecord(txRoot(t, tx), rec); err != nil {
		t.Fatal(err)
	}
	// Simulate an interrupted write of the next entry with a recognizable stale copy.
	b, err := os.ReadFile(filepath.Join(tx.root, "record"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tx.root, "record.new"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := tx.run(false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(tx.root, "record.new")); !os.IsNotExist(err) {
		t.Fatalf("temp remains: %v", err)
	}
}

func txRoot(t *testing.T, tx dataTransaction) int {
	t.Helper()
	fd, err := transactionDirectory(tx.root, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Close(fd) })
	return fd
}

func TestTransactionTempWithoutRecord(t *testing.T) {
	tx := fixtureTransaction(t)
	fd := txRoot(t, tx)
	rec := transactionRecord{1, tx.before.Identity, tx.before.Owner, "apply", "pending"}
	if err := tx.syncRecord(fd, rec); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(tx.root, "record"), filepath.Join(tx.root, "record.new")); err != nil {
		t.Fatal(err)
	}
	if err := tx.run(false, nil); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionDivergentValidTemp(t *testing.T) {
	for _, phase := range []string{"pending-apply", "done-apply"} {
		t.Run(phase, func(t *testing.T) {
			tx := fixtureTransaction(t)
			if err := tx.run(false, nil); err != nil {
				t.Fatal(err)
			}
			direction, state := "apply", "pending"
			if phase == "done-apply" {
				direction, state = "rollback", "pending"
			}
			rec := transactionRecord{1, tx.before.Identity, tx.before.Owner, direction, state}
			b, err := json.Marshal(rec)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tx.root, "record.new"), append(b, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			if err := tx.run(phase == "done-apply", nil); err != nil {
				t.Fatal("stranded recognized transition:", err)
			}
		})
	}
}

func TestTransactionRefusesUnknownTemp(t *testing.T) {
	tx := fixtureTransaction(t)
	if err := os.WriteFile(filepath.Join(tx.root, "record.new"), []byte("unknown"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := tx.run(false, nil); err == nil {
		t.Fatal("unknown temp accepted")
	}
	if _, err := os.Lstat(filepath.Join(tx.root, "record.new")); err != nil {
		t.Fatal("uncertain evidence removed", err)
	}
}

func TestTransactionRollbackInterruptions(t *testing.T) {
	for _, boundary := range []string{"before_pending", "after_pending", "before_mutation", "after_mutation", "after_target_sync", "before_completion", "after_completion"} {
		t.Run(boundary, func(t *testing.T) {
			tx := fixtureTransaction(t)
			if err := tx.run(false, nil); err != nil {
				t.Fatal(err)
			}
			err := tx.run(true, func(s string) error {
				if s == boundary {
					return os.ErrClosed
				}
				return nil
			})
			if err != os.ErrClosed {
				t.Fatalf("hook: %v", err)
			}
			if boundary == "before_pending" {
				if err := tx.run(true, nil); err != nil {
					t.Fatal(err)
				}
			} else if err := tx.run(true, nil); err != nil {
				t.Fatal(err)
			}
			if err := tx.run(true, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTransactionLockContention(t *testing.T) {
	tx := fixtureTransaction(t)
	lock, err := os.OpenFile(filepath.Join(tx.root, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err := tx.run(false, nil); err == nil {
		t.Fatal("contended lock accepted")
	}
}

func TestTransactionRefusesSpecialBitsOnJournalFiles(t *testing.T) {
	for _, surface := range []string{"lock", "record", "record.new"} {
		for _, special := range []os.FileMode{os.ModeSetuid, os.ModeSetgid, os.ModeSticky} {
			t.Run(fmt.Sprintf("%s-%v", surface, special), func(t *testing.T) {
				tx := fixtureTransaction(t)
				path := filepath.Join(tx.root, surface)
				if surface == "lock" {
					if err := os.WriteFile(path, nil, 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(path, recordBytes(t, tx, "apply", "pending"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Chmod(path, 0600|special); err != nil {
					t.Fatal(err)
				}
				info, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0600 || info.Mode()&special == 0 {
					t.Skipf("filesystem did not retain requested mode: %v", info.Mode())
				}
				if err := tx.run(false, nil); err == nil {
					t.Fatal("special-bit journal file accepted")
				}
			})
		}
	}
}

func TestTransactionWrongIdentityAndModes(t *testing.T) {
	for _, surface := range []string{"data", "root", "lock", "record"} {
		for _, mode := range []os.FileMode{0755, 0775 | os.ModeSetgid, 0775 | os.ModeSetuid, 0775 | os.ModeSticky} {
			t.Run(fmt.Sprintf("%s-%v", surface, mode), func(t *testing.T) {
				tx := fixtureTransaction(t)
				path := tx.data
				switch surface {
				case "root":
					path = tx.root
				case "lock":
					path = filepath.Join(tx.root, "lock")
					if err := os.WriteFile(path, nil, 0600); err != nil {
						t.Fatal(err)
					}
				case "record":
					path = filepath.Join(tx.root, "record")
					if err := tx.syncRecord(txRoot(t, tx), transactionRecord{1, tx.before.Identity, tx.before.Owner, "apply", "pending"}); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
				if err := tx.run(false, nil); err == nil {
					t.Fatal("unexpected mode accepted")
				}
			})
		}
	}
	for _, field := range []string{"owner", "group"} {
		t.Run(field, func(t *testing.T) {
			tx := fixtureTransaction(t)
			if field == "owner" {
				tx.before.Owner++
			} else {
				if err := os.Chown(tx.data, -1, os.Getgid()+1); err != nil {
					t.Skipf("cannot change fixture group without privilege: %v", err)
				}
			}
			if err := tx.run(false, nil); err == nil {
				t.Fatal("wrong identity accepted")
			}
		})
	}
}

func TestTransactionTempAcrossDirections(t *testing.T) {
	for _, scenario := range []string{"done-rollback-pending-apply", "pending-rollback-done-temp"} {
		t.Run(scenario, func(t *testing.T) {
			tx := fixtureTransaction(t)
			if err := tx.run(false, nil); err != nil {
				t.Fatal(err)
			}
			if scenario == "done-rollback-pending-apply" {
				if err := tx.run(true, nil); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(tx.root, "record.new"), recordBytes(t, tx, "apply", "pending"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := tx.run(false, nil); err != nil {
					t.Fatal(err)
				}
				if err := tx.run(false, nil); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := tx.run(true, func(s string) error {
					if s == "after_mutation" {
						return os.ErrClosed
					}
					return nil
				}); err != os.ErrClosed {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(tx.root, "record.new"), recordBytes(t, tx, "rollback", "done"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := tx.run(true, nil); err != nil {
					t.Fatal(err)
				}
				if err := tx.run(true, nil); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func recordBytes(t *testing.T, tx dataTransaction, direction, phase string) []byte {
	t.Helper()
	b, err := json.Marshal(transactionRecord{1, tx.before.Identity, tx.before.Owner, direction, phase})
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func TestTransactionPermissionDenial(t *testing.T) {
	for _, surface := range []string{"root", "record", "journal-sync"} {
		t.Run(surface, func(t *testing.T) {
			tx := fixtureTransaction(t)
			switch surface {
			case "root":
				if err := os.Chmod(tx.root, 0000); err != nil {
					t.Fatal(err)
				}
			case "record":
				if err := tx.syncRecord(txRoot(t, tx), transactionRecord{1, tx.before.Identity, tx.before.Owner, "apply", "pending"}); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(tx.root, "record"), 0000); err != nil {
					t.Fatal(err)
				}
			case "journal-sync":
				tx.fsync = func(fd int) error { return syscall.EACCES }
			}
			if err := tx.run(false, nil); err == nil {
				t.Fatal("permission denial accepted")
			}
		})
	}
}

func TestTransactionRollbackSyncFailures(t *testing.T) {
	for _, site := range []string{"root", "pending-file", "pending-directory", "target", "verified-target", "parent", "completion-file", "completion-directory"} {
		t.Run(site, func(t *testing.T) {
			tx := fixtureTransaction(t)
			if err := tx.run(false, nil); err != nil {
				t.Fatal(err)
			}
			index := map[string]int{"root": 1, "pending-file": 2, "pending-directory": 3, "target": 4, "verified-target": 5, "parent": 6, "completion-file": 7, "completion-directory": 8}[site]
			calls := 0
			tx.fsync = func(fd int) error {
				calls++
				if calls == index {
					return syscall.EIO
				}
				return syscall.Fsync(fd)
			}
			if err := tx.run(true, nil); err == nil {
				t.Fatal("rollback sync failure accepted")
			}
			tx.fsync = nil
			if err := tx.run(true, nil); err != nil {
				t.Fatal("retry:", err)
			}
			if err := tx.run(true, nil); err != nil {
				t.Fatal("idempotence:", err)
			}
		})
	}
}

func TestTransactionJournalOperationFailures(t *testing.T) {
	for _, direction := range []string{"apply", "rollback"} {
		for _, phase := range []string{"pending", "done"} {
			for _, operation := range []string{"write-error", "short-write", "rename"} {
				t.Run(direction+"/"+phase+"/"+operation, func(t *testing.T) {
					tx := fixtureTransaction(t)
					rollback := direction == "rollback"
					if rollback {
						if err := tx.run(false, nil); err != nil {
							t.Fatal(err)
						}
					}
					pre, post := os.FileMode(0775), os.FileMode(0700)
					if rollback {
						pre, post = post, pre
					}
					tx.writeRecord = func(fd int, b []byte) (int, error) {
						if operation == "write-error" {
							return 0, syscall.EIO
						}
						if operation == "short-write" {
							return len(b) - 1, nil
						}
						return syscall.Write(fd, b)
					}
					tx.renameRecord = func(fd int) error {
						if operation == "rename" {
							return syscall.EIO
						}
						return syscall.Renameat(fd, "record.new", fd, "record")
					}
					// A completion fault needs a durable pending record and completed mutation.
					if phase == "done" {
						tx.writeRecord = nil
						tx.renameRecord = nil
						err := tx.run(rollback, func(s string) error {
							if s == "before_completion" {
								return os.ErrClosed
							}
							return nil
						})
						if err != os.ErrClosed {
							t.Fatalf("setup: %v", err)
						}
						tx.writeRecord = func(fd int, b []byte) (int, error) {
							if operation == "write-error" {
								return 0, syscall.EIO
							}
							if operation == "short-write" {
								return len(b) - 1, nil
							}
							return syscall.Write(fd, b)
						}
						tx.renameRecord = func(fd int) error {
							if operation == "rename" {
								return syscall.EIO
							}
							return syscall.Renameat(fd, "record.new", fd, "record")
						}
					}
					if err := tx.run(rollback, nil); err == nil {
						t.Fatal("injected journal failure accepted")
					}
					info, err := os.Lstat(tx.data)
					if err != nil {
						t.Fatal(err)
					}
					want := pre
					if phase == "done" {
						want = post
					}
					if info.Mode().Perm() != want {
						t.Fatalf("mutation before durable pending or lost post-state: %v want %v", info.Mode(), want)
					}
					if phase == "pending" {
						record, exists, err := readRecord(txRoot(t, tx))
						if err != nil {
							t.Fatal(err)
						}
						if rollback && (!exists || record.Direction != "apply" || record.Phase != "done") {
							t.Fatal("lost prior durable apply")
						}
						if !rollback && exists {
							t.Fatal("pending unexpectedly durable")
						}
					}
					tx.writeRecord = nil
					tx.renameRecord = nil
					// Partial temporary JSON is ambiguous and must be preserved and refused.
					if operation != "rename" {
						if err := tx.run(rollback, nil); err == nil {
							t.Fatal("incomplete temp accepted")
						}
						if _, err := os.Lstat(filepath.Join(tx.root, "record.new")); err != nil {
							t.Fatal("incomplete evidence lost:", err)
						}
						return
					}
					if err := tx.run(rollback, nil); err != nil {
						t.Fatalf("safe retry: %v", err)
					}
					if err := tx.run(rollback, nil); err != nil {
						t.Fatalf("idempotent retry: %v", err)
					}
				})
			}
		}
	}
}

func TestTransactionRoundTrip(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(cwd, "transaction-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	data, journal := filepath.Join(root, "data"), filepath.Join(root, "journal")
	if err := os.Mkdir(data, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(data, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(journal, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Lstat(data)
	if err != nil {
		t.Fatal(err)
	}
	id, owner, _ := metadata(f)
	tx := dataTransaction{data: data, root: journal, before: Condition{id, owner, 0775}}
	if err := tx.run(false, nil); err != nil {
		t.Fatal(err)
	}
	f, _ = os.Lstat(data)
	if f.Mode().Perm() != 0700 {
		t.Fatal("apply mode")
	}
	if err := tx.run(true, nil); err != nil {
		t.Fatal(err)
	}
	f, _ = os.Lstat(data)
	if f.Mode().Perm() != 0775 {
		t.Fatal("rollback mode")
	}
}
