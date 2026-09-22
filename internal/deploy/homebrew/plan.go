//go:build linux

package homebrew

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"syscall"
)

// ContentObservation is a non-authorizing release-byte comparison.
type ObjectState struct {
	Identity Identity
	Owner    uint32
	Mode     uint32
}
type Inventory struct {
	Package ObjectState
	Config  ObjectState
}
type ContentObservation struct {
	seal               *observationSeal
	Inventory          Inventory
	Blockers           []string
	Code               string
	Version            string
	Architecture       string
	ArchiveSHA256      string
	GatewaySHA256      string
	PackageIdentity    Identity
	RuntimeConfig      string
	SystemdTrust       string
	ApplyEligible      bool
	IdentityContinuity string
}

func (p ContentObservation) String() string {
	return p.Code + " runtime/config=" + p.RuntimeConfig + " systemd=" + p.SystemdTrust
}

type observationSeal struct{ digest [32]byte }

func observationDigest(o ContentObservation) [32]byte {
	// JSON includes every exported observation field and excludes the private seal.
	bytes, _ := json.Marshal(o)
	return sha256.Sum256(bytes)
}

func sealObservation(o *ContentObservation) {
	o.seal = &observationSeal{digest: observationDigest(*o)}
}

func validObservation(o ContentObservation) bool {
	return o.seal != nil && o.seal.digest == observationDigest(o)
}

func refusal(code string) ContentObservation {
	return ContentObservation{Code: code, RuntimeConfig: "NOT VERIFIED", SystemdTrust: "NOT VERIFIED", IdentityContinuity: "NOT PROVED"}
}

type evidence struct{ archive, member string }

var pinned = map[string]evidence{
	"linux-amd64": {"bac8c7f75fb3f5b2dfc7a235fa0fc86165f83ae3cc6e6a7efe3fa9bc5b4e959b", "025db883cc0bd424c3c523c6425ea96adf64456b9b3e136bf31c3c280528fac5"},
	"linux-arm64": {"8874cd97582b582b28fc212014d9c24f043b540a01e2e5a42f87b68c83c8578f", "60a6acd86e65d5ccfbbf42bc27da72196ad65a1e941a8648def65a63c203d8a5"},
}

// CheckContent checks a selected pinned release member against a bounded local open.
// The returned observation never authorizes a pathname operation or activation.
func CheckContent(p Profile, version, arch string) ContentObservation {
	return observeWith(p, version, arch, os.Lstat, func(path string) ([]byte, error) { return readPackage(path, p.PackageIdentity) })
}
func observeWith(p Profile, version, arch string, stat func(string) (os.FileInfo, error), read func(string) ([]byte, error)) ContentObservation {
	ev, ok := pinned[arch]
	if version != "v0.6.5" || !ok {
		return refusal("unsupported_evidence")
	}
	return observeWithEvidence(p, version, arch, ev, stat, read)
}

// observeWithEvidence is a package-private synthetic evidence seam for hermetic tests.
// Production enters only through the immutable pinned selector above.
func observeWithEvidence(p Profile, version, arch string, ev evidence, stat func(string) (os.FileInfo, error), read func(string) ([]byte, error)) ContentObservation {
	if ev.archive == "" || len(ev.member) != 64 {
		return refusal("unsupported_evidence")
	}
	if r := inspectWith(p, stat); r.Code != "inspection_only" {
		return refusal(r.Code)
	}
	if read == nil {
		return refusal("unavailable_package")
	}
	bytes, err := read(p.Package)
	if err != nil {
		return refusal("unavailable_package")
	}
	digest := sha256.Sum256(bytes)
	if hex.EncodeToString(digest[:]) != ev.member {
		return refusal("content_mismatch")
	}
	// A second pathname observation does not prove continuity with the opened file.
	f, err := stat(p.Package)
	if err != nil {
		return refusal("unavailable_package")
	}
	id, _, ok := metadata(f)
	if !ok || id != p.PackageIdentity || !f.Mode().IsRegular() {
		return refusal("stale_package")
	}
	config, err := stat(p.Config)
	if err != nil {
		return refusal("unavailable_config")
	}
	configID, configOwner, valid := metadata(config)
	if !valid || !config.Mode().IsRegular() {
		return refusal("unsafe_config")
	}
	_, owner, _ := metadata(f)
	result := ContentObservation{Code: "content_equal", Version: version, Architecture: arch, ArchiveSHA256: ev.archive, GatewaySHA256: ev.member, PackageIdentity: id,
		Inventory:     Inventory{Package: ObjectState{Identity: id, Owner: owner, Mode: uint32(f.Mode().Perm())}, Config: ObjectState{Identity: configID, Owner: configOwner, Mode: uint32(config.Mode().Perm())}},
		Blockers:      []string{"homebrew_receipt_unverified", "config_semantics_unverified", "data_state_unverified", "service_state_unverified", "network_isolation_unverified", "future_path_identity_unproved"},
		RuntimeConfig: "NOT VERIFIED", SystemdTrust: "NOT VERIFIED", IdentityContinuity: "NOT PROVED"}
	sealObservation(&result)
	return result
}

// readPackage traverses from / using no-follow directory handles and bounds reads.
func readPackage(path string, expected Identity) ([]byte, error) {
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if fd >= 0 {
			_ = syscall.Close(fd)
		}
	}()
	parts := splitAbsolute(path)
	if len(parts) == 0 {
		return nil, syscall.EINVAL
	}
	if err := checkOpened(fd, "/", true); err != nil {
		return nil, err
	}
	parent := ""
	for i, part := range parts {
		flags := syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= syscall.O_DIRECTORY
		}
		next, e := syscall.Openat(fd, part, flags, 0)
		if e != nil {
			return nil, e
		}
		_ = syscall.Close(fd)
		fd = next
		parent += "/" + part
		if err := checkOpened(fd, parent, i < len(parts)-1); err != nil {
			return nil, err
		}
	}
	file := os.NewFile(uintptr(fd), "gateway")
	fd = -1
	defer file.Close()
	info, err := file.Stat()
	id, _, valid := metadata(info)
	if err != nil || !valid || id != expected || !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return nil, syscall.EINVAL
	}
	data, err := io.ReadAll(io.LimitReader(file, 128<<20+1))
	if err != nil || len(data) > 128<<20 {
		return nil, syscall.EINVAL
	}
	after, err := file.Stat()
	afterID, _, valid := metadata(after)
	if err != nil || !valid || afterID != expected || !after.Mode().IsRegular() || after.Size() != int64(len(data)) || after.Mode() != info.Mode() {
		return nil, syscall.EINVAL
	}
	return data, nil
}

// checkOpened rejects unsafe handle metadata and changed pathname snapshots.
func checkOpened(fd int, path string, directory bool) error {
	var opened syscall.Stat_t
	if err := syscall.Fstat(fd, &opened); err != nil {
		return syscall.EINVAL
	}
	snapshot, err := os.Lstat(path)
	if err != nil {
		return syscall.EINVAL
	}
	id, owner, valid := metadata(snapshot)
	if !valid || id != (Identity{Device: uint64(opened.Dev), Inode: opened.Ino}) || owner != opened.Uid || uint32(snapshot.Mode().Perm()) != opened.Mode&0777 {
		return syscall.EINVAL
	}
	if opened.Uid != 0 && opened.Uid != uint32(os.Getuid()) || opened.Mode&0022 != 0 {
		return syscall.EINVAL
	}
	if directory {
		if opened.Mode&syscall.S_IFMT != syscall.S_IFDIR || !snapshot.IsDir() {
			return syscall.EINVAL
		}
	} else if opened.Mode&syscall.S_IFMT != syscall.S_IFREG || !snapshot.Mode().IsRegular() {
		return syscall.EINVAL
	}
	return nil
}
func splitAbsolute(path string) []string {
	if len(path) == 0 || path[0] != '/' {
		return nil
	}
	var result []string
	start := 1
	for i := 1; i <= len(path); i++ {
		if i == len(path) || path[i] == '/' {
			if i == start {
				return nil
			}
			part := path[start:i]
			if part == "." || part == ".." {
				return nil
			}
			result = append(result, part)
			start = i + 1
		}
	}
	return result
}
