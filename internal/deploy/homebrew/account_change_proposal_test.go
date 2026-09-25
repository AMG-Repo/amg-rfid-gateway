//go:build linux

package homebrew

import "testing"

func sealedContentFixture(t *testing.T) ContentObservation {
	t.Helper()
	o := ContentObservation{Code: "content_equal", Inventory: Inventory{Config: ObjectState{Identity: Identity{Device: 1, Inode: 2}, Owner: 1000, Mode: 0600}}}
	sealObservation(&o)
	return o
}

func TestAccountChangeProposalRejectsContentEvidence(t *testing.T) {
	sealed := sealedContentFixture(t)
	if !validObservation(sealed) || sealed.Code != "content_equal" {
		t.Fatal("fixture must be a sealed equal observation")
	}
	cases := []struct {
		name        string
		observation ContentObservation
		code        accountRefusal
	}{
		{"sealed equal", sealed, accountMissingTreeInventory},
		{"empty", ContentObservation{}, accountUntrustedContent},
		{"unsupported", refusal("unsupported_evidence"), accountUntrustedContent},
		{"mutated", func() ContentObservation { o := sealed; o.Inventory.Config.Owner++; return o }(), accountUntrustedContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := proposeAccountChange(tc.observation)
			if p.Code != tc.code || p.ApplyEligible || len(p.Transitions) != 0 || len(p.Blockers) == 0 || p.Blockers[0] != tc.code {
				t.Fatalf("unexpected account proposal: %+v", p)
			}
		})
	}
}

func TestForgedSealedContentCannotAuthorizeAccountChange(t *testing.T) {
	// In-package code can invent a content-equal observation and seal it. Even
	// matching package/config identity and a claimed eligible status prove neither
	// whole-tree membership nor the intended account's identity.
	forged := ContentObservation{
		Code: "content_equal",
		Inventory: Inventory{
			Package: ObjectState{Identity: Identity{Device: 1, Inode: 2}, Owner: 1000, Mode: 0755},
			Config:  ObjectState{Identity: Identity{Device: 1, Inode: 3}, Owner: 1000, Mode: 0600},
		},
		ApplyEligible: true,
	}
	sealObservation(&forged)
	if !validObservation(forged) {
		t.Fatal("forged fixture must have a valid content seal")
	}
	p := proposeAccountChange(forged)
	if p.Code != accountMissingTreeInventory || p.ApplyEligible || len(p.Transitions) != 0 ||
		len(p.Blockers) != 2 || p.Blockers[0] != accountMissingTreeInventory || p.Blockers[1] != accountMissingTargetIdentity {
		t.Fatalf("forged content promoted to account-change authority: %+v", p)
	}

	forged.Inventory.Config.Owner++ // The original seal cannot authorize mutated evidence either.
	if validObservation(forged) {
		t.Fatal("mutated fixture unexpectedly retained its seal")
	}
	p = proposeAccountChange(forged)
	if p.Code != accountUntrustedContent || p.ApplyEligible || len(p.Transitions) != 0 ||
		len(p.Blockers) != 1 || p.Blockers[0] != accountUntrustedContent {
		t.Fatalf("mutated content promoted to account-change authority: %+v", p)
	}
}
