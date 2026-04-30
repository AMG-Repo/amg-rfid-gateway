package version

import (
	"testing"
)

func TestVersion_DefaultValue(t *testing.T) {
	// The Version variable should default to "dev"
	if Version != "dev" {
		t.Errorf("expected Version to be 'dev', got '%s'", Version)
	}
}
