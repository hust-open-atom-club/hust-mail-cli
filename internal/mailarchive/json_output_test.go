package mailarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONOutputPreservesReadableCharacters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "message.eml")
	message := "From: Alice <alice@example.com>\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: 教职 <通知> & \"资料\"\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=parts\r\n\r\n" +
		"--parts\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n教职通知\r\n" +
		"--parts\r\nContent-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename=\"教职<通知>&资料.txt\"\r\n\r\nattachment\r\n--parts--\r\n"
	if err := os.WriteFile(path, []byte(message), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		run  func(io.Writer) error
	}{
		{"search", func(w io.Writer) error { return Search(ctx, dir, SearchOptions{Query: "教职"}, w) }},
		{"list", func(w io.Writer) error { return List(dir, w) }},
		{"attachments list", func(w io.Writer) error { return ListAttachments(ctx, path, w) }},
		{"attachments export", func(w io.Writer) error { return ExportAttachments(ctx, path, t.TempDir(), 0, w) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := tc.run(&out); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(out.String(), "\n") || bytes.Count(out.Bytes(), []byte("\n")) != 1 {
				t.Fatalf("expected one JSON line, got %q", out.String())
			}
			if !json.Valid(out.Bytes()) {
				t.Fatalf("invalid JSON: %q", out.String())
			}
			for _, want := range []string{"教职", "<", ">", "&"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing literal %q: %s", want, out.String())
				}
			}
			if tc.name == "search" || tc.name == "list" {
				var row map[string]any
				if err := json.Unmarshal(out.Bytes(), &row); err != nil {
					t.Fatal(err)
				}
				if row["subject"] != "教职 <通知> & \"资料\"" {
					t.Errorf("subject did not round trip: %v", row["subject"])
				}
			}
		})
	}
}
