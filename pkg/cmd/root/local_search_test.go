package root

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchDateFlagsAndListDiagnostics(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	for _, tc := range []struct{ name, date string }{
		{"a-old", "Tue, 20 Oct 2026 09:00:00 +0000"},
		{"z-new", "Thu, 22 Oct 2026 09:00:00 +0000"},
	} {
		if err := os.WriteFile(filepath.Join(dir, tc.name+".eml"), []byte("Subject: "+tc.name+"\r\nDate: "+tc.date+"\r\n\r\nneedle"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, extra := range [][]string{{"--sort", "newest", "--limit", "1"}, {"--after", "2026-10-22", "--before", "2026-10-23"}} {
		var out, warnings bytes.Buffer
		cmd := NewCmdRoot(&out, &warnings)
		args := []string{"search", "--json", "--no-index", "--dir", dir}
		args = append(args, extra...)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var row map[string]any
		if err := json.Unmarshal(out.Bytes(), &row); err != nil || row["subject"] != "z-new" || warnings.Len() != 0 {
			t.Fatalf("flags not applied: %s %s %v", &out, &warnings, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.eml"), []byte("invalid header\r\n\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, warnings bytes.Buffer
	cmd := NewCmdRoot(&out, &warnings)
	cmd.SetArgs([]string{"list", "--json", "--dir", dir})
	if err := cmd.Execute(); err != nil || strings.Count(out.String(), "\n") != 2 || !strings.Contains(warnings.String(), "skipped=1") {
		t.Fatalf("list diagnostics lost: %v %s %s", err, &out, &warnings)
	}
}
