//go:build linux

package homebrew

// ServiceBoundary describes proposed action classes; it grants no authority and
// neither queries nor controls a service manager. File preparation is distinct
// from unit installation and from start/enable activation.
type ServiceBoundary struct {
	FilePreparationProposed bool
	UnitTransitionProposed  bool
	StartProposed           bool
	EnableProposed          bool
	ActivationEligible      bool
}

func modelServiceBoundary(plan ChangePlan) ServiceBoundary {
	var boundary ServiceBoundary
	if plan.Code != "conditional_proposal" {
		return boundary
	}
	for _, transition := range plan.Transitions {
		if transition.Surface == "data" && transition.Change == "restrict_mode_existing_directory" {
			boundary.FilePreparationProposed = true
		}
	}
	return boundary
}
