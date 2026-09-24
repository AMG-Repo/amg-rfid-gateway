//go:build linux

package homebrew

import (
	"crypto/sha256"
	"io"
	"os"
	"syscall"

	"gopkg.in/yaml.v3"
)

const fixedSocket = "/run/amg-rfid-gateway/gateway.sock"
const maxConfigBytes = 1 << 20

// ConfigPolicy is a sanitized comparison, never an authorization or path safety proof.
type ConfigPolicy struct {
	Code                   string
	DataEqual, SocketEqual bool
}

// ObserveConfigPolicy compares the selected snapshot's previously observed bytes.
func ObserveConfigPolicy(observation ContentObservation, selected Selection) ConfigPolicy {
	refuse := func(code string) ConfigPolicy { return ConfigPolicy{Code: code} }
	if !validObservation(observation) || observation.Code != "content_equal" || !observation.configObserved || selected.ConfigBefore != observation.Inventory.Config || selected.Config != observation.configPath {
		return refuse("untrusted_config_observation")
	}
	if !validSelectedPath(selected.Config) || !validSelectedPath(selected.Data) {
		return refuse("invalid_selection")
	}
	data, err := readConfigBytes(selected.Config, selected.ConfigBefore)
	if err != nil || sha256.Sum256(data) != observation.configDigest {
		return refuse("stale_config")
	}
	var node yaml.Node
	if err = yaml.Unmarshal(data, &node); err != nil || len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return refuse("invalid_config")
	}
	budget := 10000
	if !validPolicyNode(node.Content[0], 0, &budget) {
		return refuse("invalid_config")
	}
	mapping := node.Content[0].Content
	var dp, sp string
	seen := map[string]bool{}
	for i := 0; i < len(mapping); i += 2 {
		key, val := mapping[i], mapping[i+1]
		if seen[key.Value] {
			return refuse("invalid_config")
		}
		seen[key.Value] = true
		if key.Value == "data_path" || key.Value == "socket_path" {
			if val.Kind != yaml.ScalarNode || val.Tag != "!!str" {
				return refuse("invalid_config")
			}
			if key.Value == "data_path" {
				dp = val.Value
			} else {
				sp = val.Value
			}
		}
	}
	if !seen["data_path"] || !seen["socket_path"] {
		return refuse("invalid_config")
	}
	result := ConfigPolicy{Code: "config_policy_mismatch", DataEqual: dp == selected.Data, SocketEqual: sp == fixedSocket}
	if result.DataEqual && result.SocketEqual {
		result.Code = "config_policy_equal"
	}
	return result
}

// validPolicyNode rejects ambiguous mappings and unsupported YAML constructs before comparison.
func validPolicyNode(node *yaml.Node, depth int, budget *int) bool {
	*budget--
	if *budget < 0 || depth > 64 || node == nil || node.Kind == yaml.AliasNode {
		return false
	}
	switch node.Kind {
	case yaml.MappingNode:
		if node.Tag != "!!map" || len(node.Content)%2 != 0 {
			return false
		}
		seen := make(map[string]bool, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" || seen[key.Value] {
				return false
			}
			seen[key.Value] = true
			if !validPolicyNode(node.Content[i+1], depth+1, budget) {
				return false
			}
		}
		return true
	case yaml.SequenceNode:
		if node.Tag != "!!seq" {
			return false
		}
		for _, child := range node.Content {
			if !validPolicyNode(child, depth+1, budget) {
				return false
			}
		}
		return true
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str", "!!int", "!!float", "!!bool", "!!null", "!!timestamp":
			return true
		}
	}
	return false
}

func readConfigBytes(path string, expected ObjectState) ([]byte, error) {
	if !validSelectedPath(path) || expected.Identity.Device == 0 || expected.Identity.Inode == 0 {
		return nil, syscall.EINVAL
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if fd >= 0 {
			_ = syscall.Close(fd)
		}
	}()
	if err = checkOpened(fd, "/", true); err != nil {
		return nil, err
	}
	current := ""
	parts := splitAbsolute(path)
	if len(parts) == 0 {
		return nil, syscall.EINVAL
	}
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
		current += "/" + part
		if err = checkOpened(fd, current, i < len(parts)-1); err != nil {
			return nil, err
		}
	}
	file := os.NewFile(uintptr(fd), "config")
	fd = -1
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	id, owner, ok := metadata(before)
	if !ok || id != expected.Identity || owner != expected.Owner || uint32(before.Mode().Perm()) != expected.Mode || !before.Mode().IsRegular() || before.Size() > maxConfigBytes || before.Size() < 0 {
		return nil, syscall.EINVAL
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil || len(data) > maxConfigBytes {
		return nil, syscall.EINVAL
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	afterID, afterOwner, ok := metadata(after)
	if !ok || afterID != id || afterOwner != owner || after.Mode() != before.Mode() || after.Size() != int64(len(data)) || after.ModTime() != before.ModTime() {
		return nil, syscall.EINVAL
	}
	return data, nil
}
