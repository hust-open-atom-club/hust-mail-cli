package mailarchive

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyncTableAlignment(t *testing.T) {
	var out bytes.Buffer
	s := syncOutput{out: &out}
	if err := s.header([]string{"INBOX", "Sent Items", "收件箱"}); err != nil {
		t.Fatal(err)
	}
	for _, box := range []string{"INBOX", "Sent Items", "收件箱"} {
		if err := s.folder(box, 0, 13663, 8); err != nil {
			t.Fatal(err)
		}
	}
	want := "FOLDER        DOWNLOADED    EXISTING     IGNORED\n" +
		"\"INBOX\"                0       13663           8\n" +
		"\"Sent Items\"           0       13663           8\n" +
		"\"收件箱\"               0       13663           8\n"
	if out.String() != want {
		t.Fatalf("table columns do not align:\n%s\nwant:\n%s", &out, want)
	}
}

func TestSyncJSONRecords(t *testing.T) {
	for _, cached := range []bool{false, true} {
		root := t.TempDir()
		if cached {
			dir := filepath.Join(root, "SU5CT1g", "42")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := rememberIgnored(dir, &unavailableMessageError{uid: 7}); err != nil {
				t.Fatal(err)
			}
		}
		c, done := testPeer(t, 42, false)
		for round := 0; round < 2; round++ {
			var out, warnings bytes.Buffer
			if err := syncMailboxes(c, root, []string{"INBOX"}, time.Time{}, &out, &warnings, imapSyncOptions{json: true}); err != nil {
				t.Fatal(err)
			}
			dec := json.NewDecoder(&out)
			var folder, total syncRecord
			if err := dec.Decode(&folder); err != nil {
				t.Fatal(err)
			}
			if err := dec.Decode(&total); err != nil {
				t.Fatal(err)
			}
			if err := dec.Decode(new(any)); err != io.EOF {
				t.Fatalf("extra stdout output: %v", err)
			}
			wantDownloaded, wantExisting, wantIgnored := 1-round, round, 0
			if cached {
				wantDownloaded, wantExisting, wantIgnored = 0, 0, 1
			}
			if folder.Type != "folder" || folder.Folder != "INBOX" || total.Type != "complete" || total.Archive != root || warnings.Len() != 0 {
				t.Fatalf("incorrect result: folder=%+v total=%+v warnings=%s", folder, total, &warnings)
			}
			for _, record := range []syncRecord{folder, total} {
				if record.Downloaded != wantDownloaded || record.Existing != wantExisting || record.Ignored != wantIgnored {
					t.Fatalf("incorrect counts: %+v", record)
				}
			}
		}
		_ = c.Close()
		<-done
	}
}

func TestSyncJSONFailureHasNoCompleteRecord(t *testing.T) {
	c, done := testPeer(t, 42, true)
	defer func() { _ = c.Close(); <-done }()
	var out bytes.Buffer
	if err := syncMailboxes(c, t.TempDir(), []string{"INBOX"}, time.Time{}, &out, io.Discard, imapSyncOptions{json: true}); err == nil {
		t.Fatal("server rejection should fail sync")
	}
	if out.Len() != 0 {
		t.Fatalf("failed folder emitted successful result: %s", &out)
	}
}

func TestFoldersJSONEscaping(t *testing.T) {
	var out bytes.Buffer
	boxes := []string{"INBOX", "教职<&>\"\n"}
	if err := writeFolders(&out, boxes, true); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(out.Bytes(), []byte("\n")) != 2 || !strings.Contains(out.String(), "教职<&>") {
		t.Fatalf("not readable JSON Lines: %s", &out)
	}
	dec := json.NewDecoder(&out)
	for _, want := range boxes {
		var row map[string]string
		if err := dec.Decode(&row); err != nil || row["folder"] != want {
			t.Fatalf("folder did not round trip: %v %v", row, err)
		}
	}
}

type brokenOutput struct{}

func (brokenOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestSyncOutputErrors(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		s := syncOutput{out: brokenOutput{}, json: asJSON}
		if err := s.folder("INBOX", 0, 0, 0); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("lost output error: %v", err)
		}
		if err := s.complete("archive", 0, 0, 0, time.Time{}); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("lost completion error: %v", err)
		}
	}
}

func TestSyncJSONCutoff(t *testing.T) {
	cutoff := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	c, done := testPeerUID(t, 42, false, 7, cutoff.Add(-time.Second))
	defer func() { _ = c.Close(); <-done }()
	var out bytes.Buffer
	if err := syncMailboxes(c, t.TempDir(), []string{"INBOX"}, cutoff, &out, io.Discard, imapSyncOptions{json: true}); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&out)
	var folder, total syncRecord
	if err := dec.Decode(&folder); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&total); err != nil {
		t.Fatal(err)
	}
	if folder.Downloaded != 0 || folder.Ignored != 0 || total.Cutoff != cutoff.Format(time.RFC3339) {
		t.Fatalf("incorrect filtered results: %+v %+v", folder, total)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Fatalf("text polluted JSON output: %v", err)
	}
}
