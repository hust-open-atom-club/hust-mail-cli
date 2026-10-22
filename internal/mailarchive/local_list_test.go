package mailarchive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListContinuesPastMalformedHeaders(t *testing.T) {
	dir := t.TempDir()
	searchTestMail(t, dir, "a-good", "Subject: first\r\n\r\nbody")
	searchTestMail(t, dir, "b-bad", "invalid header\r\n\r\n")
	searchTestMail(t, dir, "c-good", "Subject: last\r\nContent-Type: multipart/mixed\r\n\r\nbroken body")
	cache := filepath.Join(dir, ".search-index")
	if err := os.Mkdir(cache, 0700); err != nil {
		t.Fatal(err)
	}
	searchTestMail(t, cache, "ignore", "invalid header\r\n\r\n")
	var out, warnings bytes.Buffer
	if err := ListContext(context.Background(), dir, &out, &warnings); err != nil {
		t.Fatal(err)
	}
	if names := resultNames(t, out.Bytes()); strings.Join(names, ",") != "a-good,c-good" {
		t.Fatalf("good headers lost: %v", names)
	}
	if strings.Count(warnings.String(), "skipping unreadable mail") != 1 || !strings.Contains(warnings.String(), "scanned=3 listed=2 skipped=1") {
		t.Fatalf("missing skip diagnostics: %s", &warnings)
	}
}

func TestListPreservesFatalErrorsAndCancellation(t *testing.T) {
	dir := t.TempDir()
	searchTestMail(t, dir, "one", "Subject: hello\r\n\r\nbody")
	if err := ListContext(context.Background(), dir, brokenOutput{}, io.Discard); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("output error lost: %v", err)
	}
	if err := ListContext(context.Background(), filepath.Join(dir, "missing"), io.Discard, io.Discard); err == nil {
		t.Fatal("missing directory accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ListContext(ctx, dir, io.Discard, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	searchTestMail(t, dir, "oversized", "Subject: "+strings.Repeat("x", (1<<20)+1)+"\r\n\r\nbody")
	var warnings bytes.Buffer
	if err := ListContext(context.Background(), dir, io.Discard, &warnings); err != nil || !strings.Contains(warnings.String(), "skipped=1") {
		t.Fatalf("oversized header not isolated: %v %s", err, &warnings)
	}
}
