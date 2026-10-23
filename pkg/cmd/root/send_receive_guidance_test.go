package root

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReceiveRejectsLegacySMTPOverrides(t *testing.T) {
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
			want := "removed config fields: protocol"
			if args[1] == "--protocol" {
				want = "unknown flag"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected legacy connection migration error, got %v", err)
			}
		}
	}
}
