//go:build linux

package homebrew

import (
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanRequiresBoundConfigEvidence(t *testing.T) {
	obs := ContentObservation{Code: "content_equal"}
	sealObservation(&obs)
	p := PlanChange(obs, Selection{})
	if p.Code != "untrusted_config_observation" {
		t.Fatalf("unobserved config accepted: %+v", p)
	}
}

func TestPlanRejectsForgedObservation(t *testing.T) {
	got := PlanChange(ContentObservation{Code: "content_equal"}, Selection{})
	if got.Code != "untrusted_observation" || len(got.Transitions) != 0 {
		t.Fatalf("forged observation accepted: %+v", got)
	}
}

type specialModeInfo struct {
	os.FileInfo
	mode os.FileMode
}

func (f specialModeInfo) Mode() os.FileMode { return f.mode }

func TestPlanRejectsSpecialDataMode(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(data)
	if err != nil {
		t.Fatal(err)
	}
	id, owner, ok := metadata(info)
	if !ok {
		t.Fatal("metadata")
	}
	config, unit, runtime := filepath.Join(root, "config"), filepath.Join(root, "unit"), filepath.Join(root, "runtime")
	for _, p := range []string{config, unit} {
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	state := func(p string) ObjectState {
		f, e := os.Lstat(p)
		if e != nil {
			t.Fatal(e)
		}
		i, o, valid := metadata(f)
		if !valid {
			t.Fatal("metadata")
		}
		return ObjectState{i, o, uint32(f.Mode().Perm())}
	}
	for _, bit := range []os.FileMode{os.ModeSetuid, os.ModeSetgid, os.ModeSticky} {
		t.Run(bit.String(), func(t *testing.T) {
			mode := info.Mode() | 0775 | bit
			if mode&fs.ModePerm != 0775 || mode&bit == 0 {
				t.Fatalf("fixture lost special bit: %v", mode)
			}
			before := ObjectState{id, owner, 0775}
			s := Selection{Config: config, Data: data, Runtime: runtime, Unit: unit, ConfigBefore: state(config), DataBefore: before, RuntimeBefore: state(runtime), UnitBefore: state(unit), DataDesiredMode: 0700}
			obs := ContentObservation{Code: "content_equal", Inventory: Inventory{Config: s.ConfigBefore}, configPath: config, configObserved: true}
			bytes, _ := os.ReadFile(config)
			obs.configDigest = sha256.Sum256(bytes)
			sealObservation(&obs)
			stat := func(path string) (os.FileInfo, error) {
				if path == data {
					return specialModeInfo{info, mode}, nil
				}
				return trustedFixtureStat(path)
			}
			p := planChangeWith(obs, s, stat)
			if len(p.Transitions) != 0 || p.Code != "unsafe_data" {
				t.Fatalf("special mode accepted: %+v", p)
			}
		})
	}
}

func TestConditionalMetadataProposal(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(cwd, "change-plan-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove fixture: %v", err)
		}
	})
	makeFile := func(name string) string {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, []byte("secret-sentinel"), 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg := filepath.Join(root, "config")
	unit := makeFile("unit")
	data := filepath.Join(root, "data")
	if err := os.WriteFile(cfg, []byte("data_path: "+data+"\nsocket_path: /run/amg-rfid-gateway/gateway.sock\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(root, "runtime")
	for _, p := range []string{data, runtime} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	state := func(p string) ObjectState {
		t.Helper()
		f, e := os.Lstat(p)
		if e != nil {
			t.Fatal(e)
		}
		id, owner, ok := metadata(f)
		if !ok {
			t.Fatal("metadata")
		}
		return ObjectState{id, owner, uint32(f.Mode().Perm())}
	}
	s := Selection{Config: cfg, Data: data, Runtime: runtime, Unit: unit, ConfigBefore: state(cfg), DataBefore: state(data), RuntimeBefore: state(runtime), UnitBefore: state(unit), DataDesiredMode: 0700}
	if err := os.Chmod(data, 0775); err != nil {
		t.Fatal(err)
	}
	s.DataBefore = state(data)
	obs := ContentObservation{Code: "content_equal", Inventory: Inventory{Config: s.ConfigBefore}, configPath: cfg, configObserved: true}
	bytes, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	obs.configDigest = sha256.Sum256(bytes)
	sealObservation(&obs)
	mutated := obs
	mutated.Inventory.Config.Mode = 0777
	if p := planChangeWith(mutated, s, trustedFixtureStat); p.Code != "untrusted_observation" {
		t.Fatalf("tampered inventory accepted: %+v", p)
	}
	mutated = obs
	mutated.PackageIdentity.Inode++
	if p := planChangeWith(mutated, s, trustedFixtureStat); p.Code != "untrusted_observation" {
		t.Fatalf("tampered identity accepted: %+v", p)
	}
	mutated = obs
	mutated.Blockers = append([]string(nil), obs.Blockers...)
	mutated.Blockers = append(mutated.Blockers, "forged")
	if p := planChangeWith(mutated, s, trustedFixtureStat); p.Code != "untrusted_observation" {
		t.Fatalf("tampered blockers accepted: %+v", p)
	}
	for _, mode := range []uint32{0500, 0000} {
		selectedUnsafe := s
		selectedUnsafe.DataDesiredMode = mode
		if p := planChangeWith(obs, selectedUnsafe, trustedFixtureStat); p.Code != "unsafe_desired_mode" {
			t.Fatalf("owner write removed at %04o: %+v", mode, p)
		}
	}
	first := planChangeWith(obs, s, trustedFixtureStat)
	second := planChangeWith(obs, s, trustedFixtureStat)
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) || first.ApplyEligible || first.Code != "conditional_proposal" || len(first.Transitions) != 1 || !first.Service.FilePreparationProposed || first.Service.UnitTransitionProposed || first.Service.StartProposed || first.Service.EnableProposed || first.Service.ActivationEligible {
		t.Fatalf("bad proposal: %s", a)
	}
	for _, blocker := range []string{"service_state_unverified", "network_isolation_unverified", "homebrew_receipt_unverified", "inverse_execution_unproved"} {
		found := false
		for _, actual := range first.Blockers {
			if actual == blocker {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("matching policy omitted blocker %s", blocker)
		}
	}
	if first.ApplyEligible {
		t.Fatal("matching policy authorized apply")
	}
	tr := first.Transitions[0]
	if tr.Change != "restrict_mode_existing_directory" || tr.Precondition.Mode != 0775 || tr.Expected.Mode != 0700 || tr.InversePrecondition != tr.Expected || tr.InverseExpected != tr.Precondition || tr.Expected.Identity != s.DataBefore.Identity {
		t.Fatalf("bad inverse: %+v", tr)
	}
	if strings.Contains(string(a), root) || strings.Contains(string(a), "secret-sentinel") {
		t.Fatal("secret/path disclosure")
	}
	s.DataBefore.Identity.Inode++
	if p := planChangeWith(obs, s, trustedFixtureStat); p.Code != "stale_data" || len(p.Transitions) != 0 {
		t.Fatalf("stale accepted: %+v", p)
	}
	s.DataBefore = state(data)
	s.UnitBefore.Identity.Inode++
	if p := planChangeWith(obs, s, trustedFixtureStat); p.Code != "stale_unit" {
		t.Fatalf("unit stale accepted: %+v", p)
	}
	s.UnitBefore = state(unit)
	s.Data = "/missing/unsafe"
	if p := planChangeWith(obs, s, trustedFixtureStat); len(p.Transitions) != 0 {
		t.Fatalf("missing accepted: %+v", p)
	}
	s.Data = filepath.Join(root, "link")
	if err := os.Symlink(data, s.Data); err != nil {
		t.Fatal(err)
	}
	if p := planChangeWith(obs, s, trustedFixtureStat); p.Code != "unsafe_data" {
		t.Fatalf("symlink accepted: %+v", p)
	}
	s.Data = "../relative"
	if p := planChangeWith(obs, s, trustedFixtureStat); p.Code != "invalid_selection" {
		t.Fatalf("relative accepted: %+v", p)
	}
}
