package mailarchive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/quotedprintable"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func localFixture(t *testing.T, dir string) string {
	t.Helper()
	raw := "Subject: " + mime.BEncoding.Encode("utf-8", "会议安排") + "\r\nFrom: " + mime.BEncoding.Encode("utf-8", "测试发件人") + " <alice@example.com>\r\nTo: bob@example.com\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=outer\r\n\r\n" +
		"--outer\r\nContent-Type: multipart/alternative; boundary=inner\r\n\r\n" +
		"--inner\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString([]byte("你好 Project Meeting\n\x1b[31m正文")) + "\r\n" +
		"--inner\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>wrong alternative</p>\r\n--inner--\r\n" +
		"--outer\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"../../report.bin\"\r\nContent-Transfer-Encoding: base64\r\n\r\nAAEC/w==\r\n" +
		"--outer\r\nContent-Type: image/png\r\nContent-Disposition: inline; filename*=utf-8''%E5%9B%BE%E7%89%87.png\r\nContent-Transfer-Encoding: base64\r\n\r\naW1hZ2U=\r\n" +
		"--outer\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=notes.txt\r\n\r\nattachment-only-keyword\r\n--outer--\r\n"
	path := filepath.Join(dir, "message.eml")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFriendlyMIMEAndAttachmentRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := localFixture(t, dir)
	original, _ := os.ReadFile(path)
	before := sha256.Sum256(original)
	var out bytes.Buffer
	if err := Show(context.Background(), path, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"会议安排", "测试发件人", "你好 Project Meeting", "Attachments:", "图片.png"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out.String(), "wrong alternative") || strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "attachment-only-keyword") {
		t.Fatal("unsafe controls, alternative HTML, or attachment rendered as body")
	}
	out.Reset()
	if err := ListAttachments(context.Background(), path, &out); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&out)
	var meta []Attachment
	for {
		var a Attachment
		err := dec.Decode(&a)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		meta = append(meta, a)
	}
	if len(meta) != 3 || meta[0].Size != 4 || meta[1].Filename != "图片.png" {
		t.Fatalf("metadata: %+v", meta)
	}
	dest := filepath.Join(dir, "export")
	if err := ExportAttachments(context.Background(), path, dest, 0, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "001-report.bin"))
	if err != nil || !bytes.Equal(got, []byte{0, 1, 2, 255}) {
		t.Fatalf("attachment bytes %v, %v", got, err)
	}
	for _, a := range meta {
		info, err := os.Stat(filepath.Join(dest, attachmentName(a)))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("export permissions", err)
		}
	}
	after, _ := os.ReadFile(path)
	if sha256.Sum256(after) != before {
		t.Fatal("source modified")
	}
	if err := ExportAttachments(context.Background(), path, filepath.Join(dir, "one"), 2, io.Discard); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "one"))
	if len(entries) != 1 || entries[0].Name() != "002-图片.png" {
		t.Fatal("index selection failed")
	}
}

func TestHTMLAndLegacyCharset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "html.eml")
	raw := "Subject: HTML\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<html><head><style>hidden-css</style></head><body><p>你好 &amp; world</p><script>hidden-script</script><div>Second<br/>Line</div></body></html>"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := walkMessage(context.Background(), path, true, nil)
	if err != nil || m.Body != "你好 & world\nSecond\nLine" {
		t.Fatalf("HTML: %q %v", m.Body, err)
	}
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("中文正文"))
	if err != nil {
		t.Fatal(err)
	}
	var qp bytes.Buffer
	writer := quotedprintable.NewWriter(&qp)
	writer.Write(gbk)
	writer.Close()
	raw = "Subject: legacy\r\nContent-Type: text/plain; charset=gbk\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n" + qp.String()
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	m, err = walkMessage(context.Background(), path, true, nil)
	if err != nil || m.Body != "中文正文" {
		t.Fatalf("GBK: %q %v", m.Body, err)
	}
}

func TestDecodedSearchFiltersLimitAndCancellation(t *testing.T) {
	dir := t.TempDir()
	path := localFixture(t, dir)
	raw, _ := os.ReadFile(path)
	os.WriteFile(filepath.Join(dir, "second.eml"), raw, 0600)
	for _, tc := range []struct {
		opts SearchOptions
		want int
	}{
		{SearchOptions{Query: "你好"}, 2},
		{SearchOptions{Query: "meeting", Subject: "会议", From: "ALICE", To: "BOB", Body: "Project", Limit: 1}, 1},
		{SearchOptions{Subject: "会议", From: "missing"}, 0},
		{SearchOptions{Body: "attachment-only-keyword"}, 0},
	} {
		var out bytes.Buffer
		if err := Search(context.Background(), dir, tc.opts, &out); err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(out.String(), "\n"); got != tc.want {
			t.Fatalf("matches %d; want %d", got, tc.want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Search(ctx, dir, SearchOptions{Query: "meeting"}, io.Discard); err != context.Canceled {
		t.Fatal("cancellation ignored", err)
	}
	for _, opts := range []SearchOptions{{}, {Query: "x", Limit: -1}} {
		if err := Search(context.Background(), dir, opts, io.Discard); err == nil {
			t.Fatal("invalid search accepted")
		}
	}
}

func TestAttachmentExportNoOverwriteOrPartialFiles(t *testing.T) {
	dir := t.TempDir()
	path := localFixture(t, dir)
	dest := filepath.Join(dir, "out")
	os.Mkdir(dest, 0700)
	outPath := filepath.Join(dest, "002-图片.png")
	os.WriteFile(outPath, []byte("original"), 0600)
	if err := ExportAttachments(context.Background(), path, dest, 0, io.Discard); err == nil {
		t.Fatal("overwrote existing output")
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 1 {
		t.Fatal("rollback left partial outputs")
	}
	data, _ := os.ReadFile(outPath)
	if string(data) != "original" {
		t.Fatal("existing file changed")
	}
	os.Remove(outPath)
	outside := filepath.Join(dir, "outside")
	os.WriteFile(outside, []byte("protected"), 0600)
	os.Symlink(outside, filepath.Join(dest, "001-report.bin"))
	if err := ExportAttachments(context.Background(), path, dest, 1, io.Discard); err == nil {
		t.Fatal("overwrote symlink")
	}
	data, _ = os.ReadFile(outside)
	if string(data) != "protected" {
		t.Fatal("symlink target changed")
	}
	bad := filepath.Join(dir, "bad.eml")
	raw, _ := os.ReadFile(path)
	os.WriteFile(bad, bytes.Replace(raw, []byte("aW1hZ2U="), []byte("%%%invalid"), 1), 0600)
	fresh := filepath.Join(dir, "fresh")
	if err := ExportAttachments(context.Background(), bad, fresh, 0, io.Discard); err == nil {
		t.Fatal("malformed encoding accepted")
	}
	entries, _ = os.ReadDir(fresh)
	if len(entries) != 0 {
		t.Fatal("failed export left files")
	}
	if err := ExportAttachments(context.Background(), path, filepath.Join(dir, "missing"), 99, io.Discard); err == nil {
		t.Fatal("missing index accepted")
	}
}

func TestTextLimitAndRawFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.eml")
	raw := "Content-Type: text/plain\r\n\r\n" + strings.Repeat("x", maxTextBytes+1)
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Show(context.Background(), path, io.Discard); err == nil || !strings.Contains(err.Error(), "8 MiB") {
		t.Fatal("missing text size limit")
	}
	if err := Read(path, io.Discard); err != nil {
		t.Fatal("raw fallback failed", err)
	}
}

func TestTextAttachmentPreservesCharsetBytes(t *testing.T) {
	original, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("中文"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, encoding, charset, kind, disposition string
		standalone                                 bool
	}{
		{"base64_gbk", "base64", "gbk", "text/plain", "attachment", false},
		{"quoted_printable_gbk", "quoted-printable", "gbk", "text/csv", "attachment", false},
		{"raw_html_gbk", "8bit", "gbk", "text/html", "attachment", false},
		{"inline_named_text", "base64", "gbk", "text/plain", "inline", false},
		{"unknown_attachment_charset", "base64", "x-unknown-charset", "text/plain", "attachment", false},
		{"standalone_attachment", "base64", "gbk", "text/plain", "attachment", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded := string(original)
			switch tc.encoding {
			case "base64":
				encoded = base64.StdEncoding.EncodeToString(original)
			case "quoted-printable":
				var buf bytes.Buffer
				qp := quotedprintable.NewWriter(&buf)
				if _, err := qp.Write(original); err != nil {
					t.Fatal(err)
				}
				if err := qp.Close(); err != nil {
					t.Fatal(err)
				}
				encoded = buf.String()
			}
			attachment := "Content-Type: " + tc.kind + "; charset=" + tc.charset + "\r\nContent-Disposition: " + tc.disposition + "; filename=original.txt\r\nContent-Transfer-Encoding: " + tc.encoding + "\r\n\r\n" + encoded
			raw := "Subject: regression\r\nMIME-Version: 1.0\r\n"
			if tc.standalone {
				raw += attachment
			} else {
				raw += "Content-Type: multipart/mixed; boundary=outer\r\n\r\n--outer\r\nContent-Type: text/plain; charset=gbk\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString(original) + "\r\n--outer\r\n" + attachment + "\r\n--outer--\r\n"
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "message.eml")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			m, err := walkMessage(context.Background(), path, true, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.standalone && m.Body != "中文" {
				t.Fatalf("body not converted to UTF-8: %q", m.Body)
			}
			if len(m.Attachments) != 1 || m.Attachments[0].Size != int64(len(original)) {
				t.Fatalf("incorrect attachment size: %+v", m.Attachments)
			}
			var listed bytes.Buffer
			if err := ListAttachments(context.Background(), path, &listed); err != nil {
				t.Fatal(err)
			}
			var meta Attachment
			if err := json.Unmarshal(bytes.TrimSpace(listed.Bytes()), &meta); err != nil {
				t.Fatal(err)
			}
			if meta.Size != 4 {
				t.Fatalf("listed size %d; want 4", meta.Size)
			}
			dest := filepath.Join(dir, "attachments")
			if err := ExportAttachments(context.Background(), path, dest, 1, io.Discard); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(dest, "001-original.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("attachment bytes changed: %x; want %x", got, original)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != raw {
				t.Fatal("source message changed", err)
			}
			if !tc.standalone {
				var results bytes.Buffer
				if err := Search(context.Background(), dir, SearchOptions{Body: "中文"}, &results); err != nil {
					t.Fatal(err)
				}
				if strings.Count(results.String(), "\n") != 1 {
					t.Fatal("decoded body search failed")
				}
			}
		})
	}
}
