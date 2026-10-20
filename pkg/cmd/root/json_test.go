package root

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/mailarchive"
	"github.com/spf13/cobra"
)

func TestGlobalJSONLocalCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "message.eml")
	raw := "Subject: 教职<&>\r\nFrom: Alice <alice@example.com>\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=parts\r\n\r\n" +
		"--parts\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n正文\r\n" +
		"--parts\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=hello.txt\r\n\r\nhello\r\n--parts--\r\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--json", "read", path},
		{"read", path, "--json"},
		{"--json", "list", "--dir", dir},
		{"search", "教职", "--dir", dir, "--json"},
		{"--json", "attachments", "list", path},
		{"attachments", "export", path, "--output", t.TempDir(), "--json"},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			cmd := NewCmdRoot(&out, &stderr)
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			var row map[string]any
			dec := json.NewDecoder(&out)
			if err := dec.Decode(&row); err != nil {
				t.Fatal(err)
			}
			if err := dec.Decode(new(any)); err != io.EOF || stderr.Len() != 0 {
				t.Fatalf("unexpected output: trailing=%v stderr=%s", err, &stderr)
			}
			if strings.Contains(strings.Join(args, " "), "read") {
				if row["subject"] != "教职<&>" || !strings.Contains(row["body"].(string), "正文") || len(row["attachments"].([]any)) != 1 {
					t.Fatalf("incorrect decoded message: %v", row)
				}
			}
		})
	}
	var out bytes.Buffer
	cmd := NewCmdRoot(&out, io.Discard)
	cmd.SetArgs([]string{"read", "--raw", "--json", path})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--raw and --json") || out.Len() != 0 {
		t.Fatalf("raw/JSON conflict was not rejected before output: %v %q", err, &out)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != raw {
		t.Fatalf("source mail changed: %v", err)
	}
}

func TestGlobalJSONReceiveOptions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"sync", "folders"} {
		for _, args := range [][]string{{"--json", name}, {name, "--json"}, {name, "--json=false"}} {
			cmd := NewCmdRoot(io.Discard, io.Discard)
			receive, _, err := cmd.Find([]string{name})
			if err != nil {
				t.Fatal(err)
			}
			receive.RunE = func(cmd *cobra.Command, _ []string) error {
				opts, err := receiveOptions(cmd, mailarchive.Options{})
				if err == nil && opts.JSON != !strings.Contains(strings.Join(args, " "), "=false") {
					t.Errorf("global flag did not reach receiving options: %v %+v", args, opts)
				}
				return err
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSearchJSONSkipsMalformedMailAndCacheFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a-bad.eml"), []byte("invalid mail header\r\n\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b-good.eml"), []byte("Subject: 教职通知\r\n\r\nneedle"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--no-index", "--rebuild-index"} {
		var out, stderr bytes.Buffer
		cmd := NewCmdRoot(&out, &stderr)
		cmd.SetArgs([]string{"--json", "search", "--body", "needle", "--dir", dir, flag})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var row map[string]any
		dec := json.NewDecoder(&out)
		if err := dec.Decode(&row); err != nil || row["subject"] != "教职通知" {
			t.Fatalf("bad mail blocked valid JSON result: %v %v", row, err)
		}
		if err := dec.Decode(new(any)); err != io.EOF || !strings.Contains(stderr.String(), "scanned=2 matched=1 skipped=1") {
			t.Fatalf("diagnostics polluted stdout or counts missing: %v %s", err, &stderr)
		}
		if flag == "--no-index" {
			if _, err := os.Stat(filepath.Join(dir, ".search-index")); !os.IsNotExist(err) {
				t.Fatal("--no-index wrote to archive", err)
			}
		}
	}
}
