package uuidutil

import (
	"regexp"
	"testing"
)

var v4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewV4_IsLowercaseVersion4UUID(t *testing.T) {
	id := NewV4()
	if !v4Pattern.MatchString(id) {
		t.Fatalf("NewV4() = %q, want a lowercase RFC 4122 version 4 UUID", id)
	}
}

func TestNewV4_IsUniquePerCall(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewV4()
		if seen[id] {
			t.Fatalf("NewV4() repeated %q", id)
		}
		seen[id] = true
	}
}
