package root

import (
	"io"
	"strings"
	"testing"
)

func TestRemovedConnectionFlagsRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, command := range []string{"sync", "folders", "send"} {
		for _, flag := range []string{"protocol", "host", "port"} {
			cmd := NewCmdRoot(io.Discard, io.Discard)
			cmd.SetArgs([]string{command, "--" + flag, "unused"})
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unknown flag") {
				t.Fatalf("%s accepted removed flag %s: %v", command, flag, err)
			}
		}
	}
}
