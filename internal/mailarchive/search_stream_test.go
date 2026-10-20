package mailarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// Attachments before, inside, and after alternative bodies must not hide later
// parts or enter the searchable text. Search does not validate their payloads;
// read/export still decode them and report invalid transfer encodings.
func TestSearchScansAttachmentBoundariesWithoutDecoding(t *testing.T) {
	for _, encoding := range []string{"base64", "quoted-printable", "x-unsupported"} {
		t.Run(encoding, func(t *testing.T) {
			dir := t.TempDir()
			raw := "Subject: streaming\r\nContent-Type: multipart/mixed; boundary=outer\r\n\r\n" +
				"--outer\r\nContent-Type: text/plain; name=before.txt\r\nContent-Transfer-Encoding: " + encoding + "\r\n\r\n!!!! attachment-only-keyword\r\n" +
				"--outer\r\nContent-Type: multipart/alternative; boundary=inner\r\n\r\n" +
				"--inner\r\nContent-Type: text/html\r\n\r\n<p>alternative-only</p>\r\n" +
				"--inner\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n中文 needle\r\n" +
				"--inner\r\nContent-Type: text/html\r\nContent-Disposition: inline; filename=inside.html\r\nContent-Transfer-Encoding: " + encoding + "\r\n\r\n!!!! attachment-only-keyword\r\n--inner--\r\n" +
				"--outer\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=after.bin\r\nContent-Transfer-Encoding: " + encoding + "\r\n\r\n!!!! attachment-only-keyword\r\n--outer--\r\n"
			path := searchTestMail(t, dir, "streaming", raw)
			for _, noIndex := range []bool{true, false, false} {
				var out, warnings bytes.Buffer
				if err := Search(context.Background(), dir, SearchOptions{Body: "中文 needle", NoIndex: noIndex}, &out, &warnings); err != nil {
					t.Fatal(err)
				}
				var row struct{ Attachments int }
				if err := json.Unmarshal(out.Bytes(), &row); err != nil || row.Attachments != 3 || warnings.Len() != 0 {
					t.Fatalf("later body or attachment count lost: %s %s %v", &out, &warnings, err)
				}
				for _, query := range []string{"attachment-only-keyword", "alternative-only"} {
					out.Reset()
					if err := Search(context.Background(), dir, SearchOptions{Body: query, NoIndex: noIndex}, &out, &warnings); err != nil || out.Len() != 0 {
						t.Fatalf("non-body text matched: %s %v", &out, err)
					}
				}
			}
			if encoding != "quoted-printable" {
				if _, err := walkMessage(context.Background(), path, true, nil); err == nil {
					t.Fatal("read stopped validating attachment encodings")
				}
				if err := ExportAttachments(context.Background(), path, t.TempDir(), 0, io.Discard); err == nil {
					t.Fatal("export accepted invalid attachment encoding")
				}
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != raw {
				t.Fatal("source message changed", err)
			}
		})
	}
}

func TestSearchUpgradesHeaderCacheUsingSourceBody(t *testing.T) {
	dir := t.TempDir()
	searchTestMail(t, dir, "one", "Subject: cached header\r\n\r\nneedle")
	var header, body bytes.Buffer
	if err := Search(context.Background(), dir, SearchOptions{Subject: "cached"}, &header); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(header.Bytes(), []byte("attachments")) {
		t.Fatal("header cache claims a known attachment count")
	}
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &body); err != nil || !bytes.Contains(body.Bytes(), []byte(`"attachments":0`)) {
		t.Fatalf("header-only cache did not upgrade: %s %v", &body, err)
	}
}

type countedMIMESource struct {
	io.Reader
	reads  int
	cancel context.CancelFunc
}

func (s *countedMIMESource) Read(p []byte) (int, error) {
	s.reads++
	if s.cancel != nil && s.reads == 2 {
		s.cancel()
	}
	return s.Reader.Read(p)
}

func TestMessageTraversalReusesHeaderStreamAndBuffersAttachmentReads(t *testing.T) {
	raw := "Subject: single pass\r\nContent-Type: multipart/mixed; boundary=parts\r\n\r\n" +
		"--parts\r\nContent-Type: text/plain\r\n\r\nneedle\r\n" +
		"--parts\r\nContent-Disposition: attachment; filename=large.bin\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		strings.Repeat("QUFB", 1<<19) + "\r\n--parts--\r\n"
	for _, metadataOnly := range []bool{false, true} {
		source := &countedMIMESource{Reader: strings.NewReader(raw)}
		r, err := newMIMEReader(context.Background(), source)
		if err != nil {
			t.Fatal(err)
		}
		header, err := readMessageHeaders(r)
		if err != nil || source.reads > 1 {
			t.Fatalf("header phase read attachment data: reads=%d err=%v", source.reads, err)
		}
		m, err := walkMessageParts(context.Background(), r, header, messageWalkOptions{body: true, attachmentMetadataOnly: metadataOnly})
		if err != nil || m.Subject != "single pass" || m.Body != "needle" || len(m.Attachments) != 1 {
			t.Fatalf("buffered header/body continuation failed: %+v %v", m, err)
		}
		if !metadataOnly && m.Attachments[0].Size != 3*(1<<19) {
			t.Fatalf("decoded attachment size changed: %d", m.Attachments[0].Size)
		}
		// Bound source reads by 64 KiB batches, rather than MIME/base64's small
		// requests. This catches regressions even on a warm filesystem cache.
		if source.reads > len(raw)/(64<<10)+4 {
			t.Fatalf("too many small source reads: %d for %d bytes", source.reads, len(raw))
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &countedMIMESource{Reader: strings.NewReader(raw), cancel: cancel}
	r, err := newMIMEReader(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	header, err := readMessageHeaders(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := walkMessageParts(ctx, r, header, messageWalkOptions{body: true, attachmentMetadataOnly: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("attachment scan ignored cancellation: %v", err)
	}
}
