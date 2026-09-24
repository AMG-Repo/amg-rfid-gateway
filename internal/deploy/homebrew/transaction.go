//go:build linux

package homebrew

import (
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// dataTransaction is fixture-only: no production caller or authorization is provided.
type dataTransaction struct {
	data, root   string
	before       Condition
	fsync        func(int) error // nil uses the system call; scoped to this transaction.
	writeRecord  func(int, []byte) (int, error)
	renameRecord func(int) error
}
type transactionRecord struct {
	Version   int      `json:"version"`
	Identity  Identity `json:"identity"`
	Owner     uint32   `json:"owner"`
	Direction string   `json:"direction"`
	Phase     string   `json:"phase"`
}
type transactionHook func(string) error

var errTransaction = errors.New("transaction refused")

func (t dataTransaction) sync(fd int) error {
	if t.fsync != nil {
		return t.fsync(fd)
	}
	return syscall.Fsync(fd)
}

func (t dataTransaction) write(fd int, b []byte) (int, error) {
	if t.writeRecord != nil {
		return t.writeRecord(fd, b)
	}
	return syscall.Write(fd, b)
}
func (t dataTransaction) rename(fd int) error {
	if t.renameRecord != nil {
		return t.renameRecord(fd)
	}
	return syscall.Renameat(fd, "record.new", fd, "record")
}

func transactionDirectory(path string, writable bool) (int, error) {
	if !validSelectedPath(path) {
		return -1, errTransaction
	}
	fd, e := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if e != nil {
		return -1, errTransaction
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		next, x := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		syscall.Close(fd)
		if x != nil {
			return -1, errTransaction
		}
		fd = next
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) != nil || st.Mode&syscall.S_IFMT != syscall.S_IFDIR || st.Uid != 0 && st.Uid != uint32(os.Getuid()) || st.Mode&0077 != 0 && writable && i == len(parts)-1 || st.Mode&0022 != 0 || st.Mode&07000 != 0 {
			syscall.Close(fd)
			return -1, errTransaction
		}
	}
	return fd, nil
}
func transactionState(fd int, name string, expected Condition, mode uint32) bool {
	var st unix.Stat_t
	if unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return false
	}
	return st.Mode&syscall.S_IFMT == syscall.S_IFDIR && st.Mode&07777 == mode && st.Uid == expected.Owner && uint64(st.Dev) == expected.Identity.Device && st.Ino == expected.Identity.Inode && st.Gid == uint32(os.Getgid())
}
func (t dataTransaction) syncRecord(fd int, r transactionRecord) error {
	b, e := json.Marshal(r)
	if e != nil {
		return errTransaction
	}
	b = append(b, '\n')
	tmp, e := syscall.Openat(fd, "record.new", syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return errTransaction
	}
	defer syscall.Close(tmp)
	if n, e := t.write(tmp, b); e != nil || n != len(b) {
		return errTransaction
	}
	if t.sync(tmp) != nil {
		return errTransaction
	}
	if t.rename(fd) != nil {
		return errTransaction
	}
	if t.sync(fd) != nil {
		return errTransaction
	}
	return nil
}

// syncTemp ensures a recovered entry is durable before promoting it to authority.
func (t dataTransaction) syncTemp(fd int) error {
	tmp, err := syscall.Openat(fd, "record.new", syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return errTransaction
	}
	defer syscall.Close(tmp)
	if t.sync(tmp) != nil {
		return errTransaction
	}
	return nil
}

func readRecord(fd int) (transactionRecord, bool, error) {
	return readNamedRecord(fd, "record")
}
func readNamedRecord(fd int, name string) (transactionRecord, bool, error) {
	var r transactionRecord
	f, e := syscall.Openat(fd, name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e == syscall.ENOENT {
		return r, false, nil
	}
	if e != nil {
		return r, false, errTransaction
	}
	file := os.NewFile(uintptr(f), "record")
	defer file.Close()
	var st syscall.Stat_t
	if syscall.Fstat(f, &st) != nil || st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Mode&07777 != 0600 || st.Uid != uint32(os.Getuid()) || st.Size > 512 {
		return r, false, errTransaction
	}
	b, e := io.ReadAll(io.LimitReader(file, 513))
	if e != nil || len(b) > 512 || len(b) == 0 || b[len(b)-1] != '\n' {
		return r, false, errTransaction
	}
	if json.Unmarshal(b[:len(b)-1], &r) != nil || r.Version != 1 || r.Direction != "apply" && r.Direction != "rollback" || r.Phase != "pending" && r.Phase != "done" {
		return r, false, errTransaction
	}
	canonical, _ := json.Marshal(r)
	if string(b[:len(b)-1]) != string(canonical) {
		return r, false, errTransaction
	}
	return r, true, nil
}

// run performs only the selected metadata transition. rollback must be explicitly selected.
// Hooks simulate crashes at durable boundaries; they never supply authority.
func (t dataTransaction) run(rollback bool, hook transactionHook) error {
	if t.before.Mode != 0775 || t.before.Owner != uint32(os.Getuid()) || t.before.Identity.Device == 0 || t.before.Identity.Inode == 0 || !validSelectedPath(t.data) || filepath.Base(t.data) == "." {
		return errTransaction
	}
	root, e := transactionDirectory(t.root, true)
	if e != nil {
		return e
	}
	defer syscall.Close(root)
	lock, e := syscall.Openat(root, "lock", syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return errTransaction
	}
	defer syscall.Close(lock)
	var lst syscall.Stat_t
	if syscall.Fstat(lock, &lst) != nil || lst.Mode&syscall.S_IFMT != syscall.S_IFREG || lst.Mode&07777 != 0600 || lst.Uid != uint32(os.Getuid()) || lst.Nlink != 1 {
		return errTransaction
	}
	if syscall.Flock(lock, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return errTransaction
	}
	defer syscall.Flock(lock, syscall.LOCK_UN)
	var named unix.Stat_t
	if unix.Fstatat(root, "lock", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || named.Ino != lst.Ino || named.Dev != lst.Dev {
		return errTransaction
	}
	if t.sync(root) != nil {
		return errTransaction
	}
	parent, e := transactionDirectory(filepath.Dir(t.data), false)
	if e != nil {
		return e
	}
	defer syscall.Close(parent)
	name := filepath.Base(t.data)
	rec, exists, e := readRecord(root)
	if e != nil {
		return e
	}
	temp, hasTemp, e := readNamedRecord(root, "record.new")
	if e != nil {
		return e
	}
	if hasTemp {
		if exists {
			// Only a byte-for-byte equivalent, already durable record is disposable.
			if temp != rec {
				// Completion may have written record.new but failed its file sync.
				// Re-sync before promotion, only on the exact post-state.
				if rec.Phase == "pending" && temp.Version == rec.Version && temp.Identity == rec.Identity && temp.Owner == rec.Owner &&
					temp.Direction == rec.Direction && temp.Phase == "done" && temp.Identity == t.before.Identity && temp.Owner == t.before.Owner &&
					((rec.Direction == "apply" && !rollback && transactionState(parent, name, t.before, 0700)) ||
						(rec.Direction == "rollback" && rollback && transactionState(parent, name, t.before, 0775))) {
					if t.syncTemp(root) != nil || syscall.Renameat(root, "record.new", root, "record") != nil || t.sync(root) != nil {
						return errTransaction
					}
					rec = temp
					goto recovered
				}
				// An interrupted next phase is recognizable only on the exact
				// durable predecessor and its expected target mode.
				valid := temp.Version == rec.Version && temp.Identity == rec.Identity && temp.Owner == rec.Owner && rec.Phase == "done" &&
					((rec.Direction == "apply" && transactionState(parent, name, t.before, 0700) &&
						((temp.Direction == "apply" && temp.Phase == "pending" && !rollback) ||
							(temp.Direction == "apply" && temp.Phase == "done" && !rollback) ||
							(temp.Direction == "rollback" && temp.Phase == "pending" && rollback))) ||
						(rec.Direction == "rollback" && transactionState(parent, name, t.before, 0775) &&
							temp.Direction == "apply" && temp.Phase == "pending" && !rollback))
				if !valid {
					return errTransaction
				}
				if temp.Direction != rec.Direction && temp.Phase == "pending" {
					if t.syncTemp(root) != nil || syscall.Renameat(root, "record.new", root, "record") != nil || t.sync(root) != nil {
						return errTransaction
					}
					rec = temp
				} else if syscall.Unlinkat(root, "record.new") != nil || t.sync(root) != nil {
					return errTransaction
				}
			} else if syscall.Unlinkat(root, "record.new") != nil || t.sync(root) != nil {
				return errTransaction
			}
		} else {
			// A pending apply on the original pre-state can be made durable before mutation.
			if temp.Direction != "apply" || temp.Phase != "pending" || temp.Identity != t.before.Identity || temp.Owner != t.before.Owner || !transactionState(parent, name, t.before, 0775) || t.syncTemp(root) != nil || t.rename(root) != nil || t.sync(root) != nil {
				return errTransaction
			}
			rec, exists = temp, true
		}
	}
recovered:
	direction := "apply"
	pre, post := uint32(0775), uint32(0700)
	if rollback {
		direction = "rollback"
		pre, post = post, pre
	}
	if exists && (rec.Identity != t.before.Identity || rec.Owner != t.before.Owner) {
		return errTransaction
	}
	if rollback && (!exists || rec.Direction == "apply" && (rec.Phase != "done" || !transactionState(parent, name, t.before, 0700))) {
		return errTransaction
	}
	if exists && rec.Direction != direction {
		if rec.Phase != "done" || !transactionState(parent, name, t.before, pre) {
			return errTransaction
		}
		exists = false
	}
	if exists && rec.Phase == "done" {
		if transactionState(parent, name, t.before, post) {
			return nil
		}
		return errTransaction
	}
	atPre := transactionState(parent, name, t.before, pre)
	atPost := transactionState(parent, name, t.before, post)
	if !atPre && !(exists && rec.Phase == "pending" && atPost) {
		return errTransaction
	}
	step := func(s string) error {
		if hook != nil {
			return hook(s)
		}
		return nil
	}
	if !exists {
		if e = step("before_pending"); e != nil {
			return e
		}
		rec = transactionRecord{1, t.before.Identity, t.before.Owner, direction, "pending"}
		if t.syncRecord(root, rec) != nil {
			return errTransaction
		}
		if e = step("after_pending"); e != nil {
			return e
		}
	}
	if atPre {
		if e = step("before_mutation"); e != nil {
			return e
		}
		target, x := syscall.Openat(parent, name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if x != nil {
			return errTransaction
		}
		defer syscall.Close(target)
		var st syscall.Stat_t
		if syscall.Fstat(target, &st) != nil || uint64(st.Dev) != t.before.Identity.Device || st.Ino != t.before.Identity.Inode || st.Uid != t.before.Owner || st.Gid != uint32(os.Getgid()) || st.Mode&07777 != pre {
			return errTransaction
		}
		if syscall.Fchmod(target, post) != nil {
			return errTransaction
		}
		if e = step("after_mutation"); e != nil {
			return e
		}
		if t.sync(target) != nil {
			return errTransaction
		}
		if e = step("after_target_sync"); e != nil {
			return e
		}
	}
	if !transactionState(parent, name, t.before, post) {
		return errTransaction
	}
	verified, e := syscall.Openat(parent, name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return errTransaction
	}
	var final syscall.Stat_t
	ok := syscall.Fstat(verified, &final) == nil && uint64(final.Dev) == t.before.Identity.Device && final.Ino == t.before.Identity.Inode && final.Uid == t.before.Owner && final.Gid == uint32(os.Getgid()) && final.Mode&07777 == post
	if !ok || t.sync(verified) != nil {
		syscall.Close(verified)
		return errTransaction
	}
	syscall.Close(verified)
	if !transactionState(parent, name, t.before, post) || t.sync(parent) != nil {
		return errTransaction
	}
	if e = step("before_completion"); e != nil {
		return e
	}
	rec.Phase = "done"
	if t.syncRecord(root, rec) != nil {
		return errTransaction
	}
	return step("after_completion")
}
