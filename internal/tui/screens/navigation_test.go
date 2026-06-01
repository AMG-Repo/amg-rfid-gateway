package screens

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMoveCursor(t *testing.T) {
	tests := []struct {
		name    string
		current int
		length  int
		delta   int
		want    int
	}{
		{name: "down moves forward", current: 0, length: 3, delta: 1, want: 1},
		{name: "up moves backward", current: 2, length: 3, delta: -1, want: 1},
		{name: "down wraps to first", current: 2, length: 3, delta: 1, want: 0},
		{name: "up wraps to last", current: 0, length: 3, delta: -1, want: 2},
		{name: "empty list is no-op", current: 0, length: 0, delta: 1, want: 0},
		{name: "negative current normalizes", current: -1, length: 3, delta: 1, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, moveCursor(tt.current, tt.length, tt.delta))
		})
	}
}
