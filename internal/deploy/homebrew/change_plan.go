//go:build linux

package homebrew

import (
	"os"
	"path/filepath"
)

// Selection names every surface explicitly. These path strings are never serialized.
type Selection struct {
	Config, Data, Runtime, Unit                         string
	ConfigBefore, DataBefore, RuntimeBefore, UnitBefore ObjectState
	DataDesiredMode                                     uint32
}

type Condition struct {
	Identity Identity
	Owner    uint32
	Mode     uint32
}
type Transition struct {
	Surface             string
	Change              string
	Precondition        Condition
	Expected            Condition
	InversePrecondition Condition
	InverseExpected     Condition
	// An inverse is a conditional proposal, not an executable recovery proof.
}
type ChangePlan struct {
	Code          string
	Transitions   []Transition
	Blockers      []string
	ApplyEligible bool
	Service       ServiceBoundary
}

// PlanChange is a pure non-authorizing metadata proposal. No service evidence is
// available to this entry point, so unit changes are never proposed.
func PlanChange(observation ContentObservation, selected Selection) ChangePlan {
	return planChangeWith(observation, selected, os.Lstat)
}
func planChangeWith(observation ContentObservation, selected Selection, stat func(string) (os.FileInfo, error)) ChangePlan {
	refuse := func(code string) ChangePlan {
		return ChangePlan{Code: code, Blockers: []string{code}, Transitions: []Transition{}}
	}
	if !validObservation(observation) || observation.Code != "content_equal" {
		return refuse("untrusted_observation")
	}
	if !observation.configObserved || observation.configPath == "" {
		return refuse("untrusted_config_observation")
	}
	surfaces := []struct {
		path      string
		before    ObjectState
		kind      string
		directory bool
	}{
		{selected.Config, selected.ConfigBefore, "config", false},
		{selected.Data, selected.DataBefore, "data", true},
		{selected.Runtime, selected.RuntimeBefore, "runtime", true},
		{selected.Unit, selected.UnitBefore, "unit", false},
	}
	for _, s := range surfaces {
		if !validSelectedPath(s.path) || s.before.Identity.Device == 0 || s.before.Identity.Inode == 0 {
			return refuse("invalid_selection")
		}
		path := s.path
		for {
			f, err := stat(path)
			if err != nil {
				if os.IsNotExist(err) {
					return refuse("missing_" + s.kind)
				}
				return refuse("unavailable_" + s.kind)
			}
			id, owner, ok := metadata(f)
			if f.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 && path == s.path {
				return refuse("unsafe_" + s.kind)
			}
			if !ok || (owner != 0 && owner != uint32(os.Getuid())) || (f.Mode().Perm()&0022 != 0 && !(path == s.path && s.kind == "data" && owner == uint32(os.Getuid()) && uint32(f.Mode().Perm()) == 0775)) || f.Mode()&os.ModeSymlink != 0 {
				return refuse("unsafe_" + s.kind)
			}
			if path == s.path {
				if id != s.before.Identity || owner != s.before.Owner || uint32(f.Mode().Perm()) != s.before.Mode {
					return refuse("stale_" + s.kind)
				}
				if s.directory && !f.IsDir() || !s.directory && !f.Mode().IsRegular() {
					return refuse("unsafe_" + s.kind)
				}
			} else if !f.IsDir() {
				return refuse("unsafe_" + s.kind)
			}
			if path == "/" {
				break
			}
			path = filepath.Dir(path)
		}
	}
	if selected.ConfigBefore != observation.Inventory.Config || selected.Config != observation.configPath {
		return refuse("stale_config")
	}
	if selected.DataDesiredMode != 0700 || (selected.DataBefore.Mode != 0700 && selected.DataBefore.Mode != 0775) {
		return refuse("unsafe_desired_mode")
	}
	policy := ObserveConfigPolicy(observation, selected)
	if policy.Code != "config_policy_equal" {
		return refuse(policy.Code)
	}
	p := ChangePlan{Code: "conditional_proposal", Blockers: []string{"config_runtime_safety_unverified", "crash_recovery_unproved", "service_state_unverified", "network_isolation_unverified", "future_path_identity_unproved", "inverse_execution_unproved", "homebrew_receipt_unverified"}}
	if selected.DataBefore.Mode != selected.DataDesiredMode {
		before := Condition{selected.DataBefore.Identity, selected.DataBefore.Owner, selected.DataBefore.Mode}
		after := Condition{selected.DataBefore.Identity, selected.DataBefore.Owner, selected.DataDesiredMode}
		p.Transitions = []Transition{{Surface: "data", Change: "restrict_mode_existing_directory", Precondition: before, Expected: after, InversePrecondition: after, InverseExpected: before}}
	}
	p.Service = modelServiceBoundary(p)
	return p
}
func validSelectedPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/"
}
