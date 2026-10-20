package mailarchive

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMissingMediaTypePreservesBodyAndBinaryAttachment(t *testing.T) {
	dir := t.TempDir()
	binary := []byte{0xff, 0, 0x12, 0x34, 0x80}
	raw := "Subject: recovered\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=parts\r\n\r\n" +
		"--parts\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>中文 BPF 正文</p>\r\n" +
		"--parts\r\nContent-Type: ;\r\n\tname=\"=?utf-8?Q?FInOYprcjR?=\"\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString(binary) + "\r\n--parts--\r\n"
	path := searchTestMail(t, dir, "missing-type", raw)
	m, err := walkMessage(context.Background(), path, true, nil)
	if err != nil || !strings.Contains(m.Body, "中文 BPF 正文") || len(m.Attachments) != 1 {
		t.Fatalf("recovery failed: %+v %v", m, err)
	}
	if m.Attachments[0].ContentType != "application/octet-stream" {
		t.Fatalf("binary part misclassified: %+v", m.Attachments)
	}
	dest := t.TempDir()
	if err := ExportAttachments(context.Background(), path, dest, 0, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "001-FInOYprcjR"))
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatal("binary attachment changed", err)
	}
	source, err := os.ReadFile(path)
	if err != nil || string(source) != raw {
		t.Fatal("source MIME modified", err)
	}
}

func TestMissingMediaTypeUsesPlainTextDefault(t *testing.T) {
	for _, header := range []string{"", "Content-Type: \r\n", "Content-Type: ; charset=utf-8\r\n"} {
		dir := t.TempDir()
		path := searchTestMail(t, dir, "plain", "Subject: default\r\n"+header+"\r\n中文 BPF")
		m, err := walkMessage(context.Background(), path, true, nil)
		if err != nil || m.Body != "中文 BPF" {
			t.Fatalf("header=%q body=%q err=%v", header, m.Body, err)
		}
	}
}

func TestInvalidCharsetFallsBackToUTF8AndInvalidatesOldIndex(t *testing.T) {
	for _, label := range []string{"a", "yes", "y"} {
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			body := "正常中英文 BPF text\n"
			raw := fmt.Sprintf("Subject: recovered\r\nContent-Type: text/plain; charset=%s\r\nContent-Transfer-Encoding: base64\r\n\r\n%s", label, base64.StdEncoding.EncodeToString([]byte(body)))
			path := searchTestMail(t, dir, "charset", raw)
			m, err := walkMessage(context.Background(), path, true, nil)
			if err != nil || m.Body != body {
				t.Fatalf("charset fallback changed UTF-8 text: %q %v", m.Body, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			cache := searchIndex{root: dir, dir: filepath.Join(dir, ".search-index")}
			if err := privateDir(cache.dir); err != nil {
				t.Fatal(err)
			}
			old := searchIndexRecord{Version: searchIndexVersion - 1, Path: "charset.eml", Size: info.Size(), ModTime: info.ModTime().UnixNano(), BodyError: "old unsupported charset failure"}
			data, err := json.Marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cache.recordPath("charset.eml"), data, 0600); err != nil {
				t.Fatal(err)
			}
			for _, noIndex := range []bool{true, false, false} {
				var out, warnings bytes.Buffer
				if err := Search(context.Background(), dir, SearchOptions{Body: "bpf", NoIndex: noIndex}, &out, &warnings); err != nil {
					t.Fatal(err)
				}
				var row map[string]any
				if err := json.Unmarshal(out.Bytes(), &row); err != nil || row["subject"] != "recovered" || warnings.Len() != 0 {
					t.Fatalf("old cached failure retained: %s %s %v", &out, &warnings, err)
				}
			}
		})
	}
}

func TestInvalidCharsetReplacementRespectsTextLimit(t *testing.T) {
	dir := t.TempDir()
	path := searchTestMail(t, dir, "invalid", "Subject: replacement\r\nContent-Type: text/plain; charset=y\r\n\r\nhello \xff BPF")
	m, err := walkMessage(context.Background(), path, true, nil)
	if err != nil || !utf8.ValidString(m.Body) || !strings.Contains(m.Body, "BPF") {
		t.Fatalf("invalid byte recovery failed: %q %v", m.Body, err)
	}
	path = searchTestMail(t, dir, "large", "Subject: large\r\nContent-Type: text/plain; charset=y\r\n\r\n"+strings.Repeat("\xff", maxTextBytes/2))
	if _, err := walkMessage(context.Background(), path, true, nil); err == nil || !strings.Contains(err.Error(), "8 MiB") {
		t.Fatalf("replacement bypassed decoded text limit: %v", err)
	}
}

func TestInvalidCharsetReplacementBytes(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"valid", "正常 UTF-8 BPF", "正常 UTF-8 BPF"},
		{"invalid_byte", "hello \xff BPF", "hello \ufffd BPF"},
		{"truncated_rune", "正文\xe4\xb8", "正文\ufffd"},
		{"invalid_sequence", "a\xe4b\x80c", "a\ufffdb\ufffdc"},
		{"buffer_boundary", strings.Repeat("x", 4095) + "中文\xff BPF", strings.Repeat("x", 4095) + "中文\ufffd BPF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			raw := "Subject: replacement\r\nContent-Type: text/plain; charset=y\r\n\r\n" + tc.input
			path := searchTestMail(t, dir, "replacement", raw)
			m, err := walkMessage(context.Background(), path, true, nil)
			if err != nil || m.Body != tc.want {
				t.Fatalf("body=%q want=%q err=%v", m.Body, tc.want, err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != raw {
				t.Fatal("source message changed", err)
			}
		})
	}
}
