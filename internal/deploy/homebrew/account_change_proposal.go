//go:build linux

package homebrew

// These shapes describe a possible per-object change, not collected evidence.
// No production constructor populates transitions from caller-provided objects.
type accountObjectKind uint8

const (
	accountDirectory accountObjectKind = iota + 1
	accountRegularFile
)

type accountObjectIdentity struct {
	RelativePath string
	Identity     Identity
	Kind         accountObjectKind
}

type accountObjectAccess struct {
	UID  uint32
	GID  uint32
	Mode uint32
}

type accountObjectTransition struct {
	Object accountObjectIdentity
	Before accountObjectAccess
	After  accountObjectAccess
}

type accountRefusal string

const (
	accountUntrustedContent      accountRefusal = "untrusted_content_observation"
	accountMissingTreeInventory  accountRefusal = "missing_whole_tree_inventory"
	accountMissingTargetIdentity accountRefusal = "missing_authenticated_target_identity"
)

type accountChangeProposal struct {
	Code          accountRefusal
	Blockers      []accountRefusal
	Transitions   []accountObjectTransition
	ApplyEligible bool
}

// proposeAccountChange cannot authenticate tree membership or the target account
// from ContentObservation, even when its package/config comparison is sealed.
func proposeAccountChange(observation ContentObservation) accountChangeProposal {
	if !validObservation(observation) || observation.Code != "content_equal" {
		return accountChangeProposal{Code: accountUntrustedContent, Blockers: []accountRefusal{accountUntrustedContent}, Transitions: []accountObjectTransition{}}
	}
	return accountChangeProposal{
		Code:        accountMissingTreeInventory,
		Blockers:    []accountRefusal{accountMissingTreeInventory, accountMissingTargetIdentity},
		Transitions: []accountObjectTransition{},
	}
}
