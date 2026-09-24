//go:build linux

package homebrew

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPolicyRefusesUnsafeFilesystemEvidence(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(cwd, "config-policy-adversarial-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("cleanup fixture: %v", err)
		}
	})
	data := filepath.Join(root, "data")
	body := []byte("data_path: " + data + "\nsocket_path: " + fixedSocket + "\n")
	state := func(path string) ObjectState {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		id, owner, ok := metadata(info)
		if !ok {
			t.Fatal("invalid fixture metadata")
		}
		return ObjectState{id, owner, uint32(info.Mode().Perm())}
	}
	observe := func(path string) (ContentObservation, Selection) {
		t.Helper()
		before := state(path)
		obs := ContentObservation{Code: "content_equal", Inventory: Inventory{Config: before}, configDigest: sha256.Sum256(body), configObserved: true, configPath: path}
		sealObservation(&obs)
		return obs, Selection{Config: path, Data: data, ConfigBefore: before}
	}
	write := func(path string, bytes []byte) {
		t.Helper()
		if err := os.WriteFile(path, bytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("explicit_tmp_ancestor", func(t *testing.T) {
		// The selected file is protected, but its explicit ancestor is not.
		parent, err := os.MkdirTemp("/tmp", "config-policy-unsafe-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(parent); err != nil {
				t.Errorf("cleanup unsafe fixture: %v", err)
			}
		})
		path := filepath.Join(parent, "config")
		write(path, body)
		obs, sel := observe(path)
		if got := ObserveConfigPolicy(obs, sel); got.Code == "config_policy_equal" {
			t.Fatal("accepted sticky world-writable ancestor")
		}
	})
	t.Run("symlinked_ancestor", func(t *testing.T) {
		actual := filepath.Join(root, "actual")
		if err := os.Mkdir(actual, 0700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(actual, link); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(actual, "config"), body)
		path := filepath.Join(link, "config")
		obs, sel := observe(path)
		if got := ObserveConfigPolicy(obs, sel); got.Code == "config_policy_equal" {
			t.Fatal("accepted symlinked ancestor")
		}
	})
	t.Run("same_bytes_new_inode", func(t *testing.T) {
		path := filepath.Join(root, "replacement")
		write(path, body)
		obs, sel := observe(path)
		replacement := filepath.Join(root, "replacement-new")
		write(replacement, body)
		if err := os.Rename(replacement, path); err != nil {
			t.Fatal(err)
		}
		if state(path).Identity == sel.ConfigBefore.Identity {
			t.Fatal("fixture did not change inode")
		}
		if got := ObserveConfigPolicy(obs, sel); got.Code == "config_policy_equal" {
			t.Fatal("accepted substituted inode")
		}
	})
	t.Run("oversize", func(t *testing.T) {
		path := filepath.Join(root, "oversize")
		large := make([]byte, maxConfigBytes+1)
		copy(large, body)
		write(path, large)
		obs, sel := observe(path)
		obs.configDigest = sha256.Sum256(large)
		sealObservation(&obs)
		if got := ObserveConfigPolicy(obs, sel); got.Code == "config_policy_equal" {
			t.Fatal("accepted oversized config")
		}
	})
}

func TestConfigPolicyRealReader(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(cwd, "config-policy-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove fixture: %v", err)
		}
	})
	cfg := filepath.Join(root, "config")
	data := filepath.Join(root, "data")
	if err := os.WriteFile(cfg, []byte("data_path: "+data+"\nsocket_path: /run/amg-rfid-gateway/gateway.sock\npassword: secret-sentinel\n"), 0600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(cfg)
	id, owner, _ := metadata(info)
	raw, _ := os.ReadFile(cfg)
	obs := ContentObservation{Code: "content_equal", Inventory: Inventory{Config: ObjectState{id, owner, 0600}}, configDigest: sha256.Sum256(raw), configObserved: true, configPath: cfg}
	sealObservation(&obs)
	s := Selection{Config: cfg, Data: data, ConfigBefore: obs.Inventory.Config}
	got := ObserveConfigPolicy(obs, s)
	if got.Code != "config_policy_equal" || !got.DataEqual || !got.SocketEqual {
		t.Fatalf("unexpected: %+v", got)
	}
	if strings.Contains(got.Code, "secret-sentinel") {
		t.Fatal("secret leaked")
	}
	for _, tc := range []struct{ name, body, code string }{
		{"duplicate", "data_path: " + data + "\ndata_path: " + data + "\nsocket_path: " + fixedSocket + "\n", "invalid_config"},
		{"nested_duplicate", "data_path: " + data + "\nsocket_path: " + fixedSocket + "\nextra: {k: 1, k: 2}\n", "invalid_config"},
		{"sequence_duplicate", "data_path: " + data + "\nsocket_path: " + fixedSocket + "\nextra: [{k: 1, k: 2}]\n", "invalid_config"},
		{"typed_root_key", "!!int data_path: " + data + "\nsocket_path: " + fixedSocket + "\n", "invalid_config"},
		{"alias", "data_path: " + data + "\nsocket_path: " + fixedSocket + "\nextra: &a [*a]\n", "invalid_config"},
		{"tagged_extra", "data_path: " + data + "\nsocket_path: " + fixedSocket + "\nextra: !custom value\n", "invalid_config"},
		{"malformed", "data_path: [unterminated\n", "invalid_config"},
		{"missing_socket", "data_path: " + data + "\n", "invalid_config"},
		{"default_socket", "data_path: " + data + "\nsocket_path: /tmp/amg-gateway.sock\n", "config_policy_mismatch"},
		{"wrong_data", "data_path: /other\nsocket_path: " + fixedSocket + "\n", "config_policy_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(cfg, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			candidate := obs
			candidate.configDigest = sha256.Sum256([]byte(tc.body))
			sealObservation(&candidate)
			result := ObserveConfigPolicy(candidate, s)
			if result.Code != tc.code || strings.Contains(result.Code, data) || strings.Contains(result.Code, "/other") {
				t.Fatalf("unsanitized/refusal: %+v", result)
			}
		})
	}
	mutated := obs
	mutated.configDigest = sha256.Sum256([]byte("forged"))
	if got := ObserveConfigPolicy(mutated, s); got.Code != "untrusted_config_observation" {
		t.Fatalf("mutated evidence accepted: %+v", got)
	}
	mutated = obs
	mutated.seal = &observationSeal{digest: sha256.Sum256([]byte("forged"))}
	if got := ObserveConfigPolicy(mutated, s); got.Code != "untrusted_config_observation" {
		t.Fatalf("substituted seal accepted: %+v", got)
	}
	wrong := s
	wrong.ConfigBefore.Identity.Inode++
	if got := ObserveConfigPolicy(obs, wrong); got.Code != "untrusted_config_observation" {
		t.Fatalf("changed selection accepted: %+v", got)
	}
	if err := os.WriteFile(cfg, []byte("data_path: "+data+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got = ObserveConfigPolicy(obs, s); got.Code == "config_policy_equal" {
		t.Fatal("changed bytes accepted")
	}
}
