package root

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSMTPReceiveGuidanceForFlagsAndConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"protocol":"smtp"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"sync", "folders"} {
		for _, args := range [][]string{{command, "--protocol", "smtp"}, {command, "--config", path}} {
			root := NewCmdRoot(io.Discard, io.Discard)
			root.SetArgs(args)
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "send command") || strings.Contains(err.Error(), "not implemented") {
				t.Fatalf("misleading SMTP receive error: %v", err)
			}
		}
	}
}
