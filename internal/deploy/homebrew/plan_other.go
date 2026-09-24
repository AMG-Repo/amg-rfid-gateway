//go:build !linux

package homebrew

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

func CheckContent(Profile, string, string) ContentObservation {
	return ContentObservation{Code: "unsupported_platform", RuntimeConfig: "NOT VERIFIED", SystemdTrust: "NOT VERIFIED", IdentityContinuity: "NOT PROVED"}
}
