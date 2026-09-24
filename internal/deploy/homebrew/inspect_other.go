//go:build !linux

package homebrew

// Non-Linux metadata has no established ownership/identity contract.
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
	return r.Code + " runtime/config=" + r.RuntimeConfig + " systemd=" + r.SystemdTrust
}
func Inspect(Profile) Result { return Result{"unsupported_platform", "NOT VERIFIED", "NOT VERIFIED"} }
