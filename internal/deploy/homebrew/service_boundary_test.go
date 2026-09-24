//go:build linux

package homebrew

import "testing"

func TestServiceBoundarySeparatesPreparationFromActivation(t *testing.T) {
	for _, tc := range []struct {
		name            string
		plan            ChangePlan
		wantPreparation bool
	}{
		{"conditional data proposal", ChangePlan{Code: "conditional_proposal", Transitions: []Transition{{Surface: "data", Change: "restrict_mode_existing_directory"}}, Blockers: []string{"service_state_unverified"}}, true},
		{"refusal", ChangePlan{Code: "stale_unit", Blockers: []string{"stale_unit"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boundary := modelServiceBoundary(tc.plan)
			if boundary.FilePreparationProposed != tc.wantPreparation || boundary.UnitTransitionProposed || boundary.StartProposed || boundary.EnableProposed || boundary.ActivationEligible {
				t.Fatalf("unexpected service boundary: %+v", boundary)
			}
		})
	}
}
