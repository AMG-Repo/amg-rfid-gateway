//go:build linux

package homebrew

import (
	"os"
	"path/filepath"
	"syscall"
)

// RuntimeCandidate identifies a local directory and a not-yet-created socket name.
// Expected is independently selected metadata, not a caller assertion of safety.
type RuntimeCandidate struct {
	Directory, Socket string
	Expected          Condition
}

// RuntimeEvidence reports metadata only; it cannot authorize creation or activation.
type RuntimeEvidence struct {
	Code              string
	DirectoryIdentity Identity
	Owner, Mode       uint32
}

// ObserveRuntimeEvidence refuses ambiguous paths and never creates any object.
func ObserveRuntimeEvidence(c RuntimeCandidate) RuntimeEvidence {
	refuse := func(code string) RuntimeEvidence { return RuntimeEvidence{Code: code} }
	if !validSelectedPath(c.Directory) || !validSelectedPath(c.Socket) || filepath.Dir(c.Socket) != c.Directory || len(c.Directory) > 4096 || len(c.Socket) > 4096 || len(splitAbsolute(c.Directory)) > 64 || len(splitAbsolute(c.Socket)) > 65 || c.Expected.Identity.Device == 0 || c.Expected.Identity.Inode == 0 || c.Expected.Mode != 0700 {
		return refuse("invalid_runtime_candidate")
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return refuse("runtime_unavailable")
	}
	defer func() { _ = syscall.Close(fd) }()
	if checkOpened(fd, "/", true) != nil {
		return refuse("unsafe_runtime_path")
	}
	current := ""
	for _, part := range splitAbsolute(c.Directory) {
		next, e := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
		if e != nil {
			return refuse("runtime_unavailable")
		}
		_ = syscall.Close(fd)
		fd = next
		current += "/" + part
		if checkOpened(fd, current, true) != nil {
			return refuse("unsafe_runtime_path")
		}
	}
	var st syscall.Stat_t
	if syscall.Fstat(fd, &st) != nil || (Identity{Device: uint64(st.Dev), Inode: st.Ino}) != c.Expected.Identity || st.Uid != c.Expected.Owner || st.Mode&07777 != 0700 {
		return refuse("stale_runtime_directory")
	}
	// The socket is a candidate name only: a pre-existing object is refused.
	if _, err := os.Lstat(c.Socket); err == nil || !os.IsNotExist(err) {
		return refuse("socket_candidate_unavailable")
	}
	if checkOpened(fd, c.Directory, true) != nil || syscall.Fstat(fd, &st) != nil || (Identity{Device: uint64(st.Dev), Inode: st.Ino}) != c.Expected.Identity || st.Uid != c.Expected.Owner || st.Mode&07777 != 0700 {
		return refuse("stale_runtime_directory")
	}
	return RuntimeEvidence{Code: "runtime_metadata_observed", DirectoryIdentity: c.Expected.Identity, Owner: st.Uid, Mode: 0700}
}
