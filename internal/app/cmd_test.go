package app

import (
	"testing"
)

func TestCmd_RejectsArgs(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{"help", "foo"} {
		cmd := NewCmd("beacon", "test")
		cmd.SetArgs([]string{arg})
		if err := cmd.Execute(); err == nil {
			t.Errorf("%s: expected error", arg)
		}
	}
}
