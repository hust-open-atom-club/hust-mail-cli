package mailarchive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

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
	for _, metadataOnly := range []bool{false} {
		source := &countedMIMESource{Reader: strings.NewReader(raw)}
		r, err := newMIMEReader(context.Background(), source)
		if err != nil {
			t.Fatal(err)
		}
		header, err := readMessageHeaders(r)
		if err != nil || source.reads > 1 {
			t.Fatalf("header phase read attachment data: reads=%d err=%v", source.reads, err)
		}
		m, err := walkMessageParts(context.Background(), r, header, messageWalkOptions{body: true})
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
	if _, err := walkMessageParts(ctx, r, header, messageWalkOptions{body: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("attachment scan ignored cancellation: %v", err)
	}
}
