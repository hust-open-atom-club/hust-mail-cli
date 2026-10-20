package mailarchive

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
)

func TestUnavailableMailDoesNotBlockOtherMessagesOrFolders(t *testing.T) {
	for _, mode := range []string{"no_literal", "uid_only", "removed", "tagged_failure"} {
		t.Run(mode, func(t *testing.T) {
			a, b := net.Pipe()
			_ = a.SetDeadline(time.Now().Add(10 * time.Second))
			_ = b.SetDeadline(time.Now().Add(10 * time.Second))
			done := make(chan struct{})
			var bodyRequests atomic.Int32
			var recovered atomic.Bool
			var validity atomic.Uint32
			validity.Store(42)
			go func() {
				defer close(done)
				defer b.Close()
				fmt.Fprint(b, "* PREAUTH [CAPABILITY IMAP4rev1] ready\r\n")
				scanner := bufio.NewScanner(b)
				other := false
				for scanner.Scan() {
					tag, command, _ := strings.Cut(scanner.Text(), " ")
					switch {
					case command == "CAPABILITY":
						fmt.Fprintf(b, "* CAPABILITY IMAP4rev1\r\n%s OK done\r\n", tag)
					case strings.HasPrefix(command, "EXAMINE "):
						other = strings.Contains(command, "Other")
						fmt.Fprintf(b, "* 2 EXISTS\r\n* OK [UIDVALIDITY %d] valid\r\n%s OK [READ-ONLY] done\r\n", validity.Load(), tag)
					case command == "UID SEARCH ALL":
						uids := "7 8"
						if other {
							uids = "9"
						}
						fmt.Fprintf(b, "* SEARCH %s\r\n%s OK done\r\n", uids, tag)
					case command == "UID FETCH 7 (UID)":
						if mode != "removed" {
							fmt.Fprint(b, "* 1 FETCH (UID 7)\r\n")
						}
						fmt.Fprintf(b, "%s OK done\r\n", tag)
					case command == "UID FETCH 7 (UID RFC822.SIZE)":
						fmt.Fprintf(b, "* 1 FETCH (UID 7 RFC822.SIZE 145300797)\r\n%s OK done\r\n", tag)
					case command == "UID FETCH 8 (UID)":
						fmt.Fprintf(b, "* 2 FETCH (UID 8)\r\n%s OK done\r\n", tag)
					case command == "UID FETCH 9 (UID)":
						fmt.Fprintf(b, "* 1 FETCH (UID 9)\r\n%s OK done\r\n", tag)
					case strings.HasPrefix(command, "FETCH ") && strings.Contains(command, "BODY.PEEK[]"):
						seq := "1"
						uid := 9
						if !other && strings.HasPrefix(command, "FETCH 1 ") {
							bodyRequests.Add(1)
							if recovered.Load() {
								body := "Subject: recovered\r\n\r\nbody\r\n"
								fmt.Fprintf(b, "* 1 FETCH (UID 7 BODY[] {%d}\r\n%s)\r\n%s OK done\r\n", len(body), body, tag)
								continue
							}
							if mode == "uid_only" {
								fmt.Fprint(b, "* 1 FETCH (UID 7)\r\n")
							}
							status := "OK"
							if mode == "tagged_failure" {
								status = "NO"
							}
							fmt.Fprintf(b, "%s %s done\r\n", tag, status)
							continue
						}
						if !other {
							seq = "2"
							uid = 8
						}
						body := "Subject: test\r\n\r\nbody\r\n"
						fmt.Fprintf(b, "* %s FETCH (BODY[] {%d}\r\n%s UID %d)\r\n%s OK done\r\n", seq, len(body), body, uid, tag)
					default:
						t.Errorf("unexpected or non-read-only command: %s", command)
						return
					}
				}
			}()
			c := imapclient.New(a, nil)
			defer func() { _ = c.Close(); <-done }()
			dir := t.TempDir()
			var out, warnings bytes.Buffer
			err := syncMailboxes(c, dir, []string{"INBOX", "Other"}, time.Time{}, &out, &warnings)
			if mode == "tagged_failure" {
				if err == nil {
					t.Fatal("ignored protocol failure")
				}
				if strings.Contains(out.String(), "Other") {
					t.Fatal("continued after protocol failure")
				}
				return
			}
			if !strings.Contains(out.String(), "Complete: downloaded=2 ignored=1") || strings.Contains(out.String(), "Incomplete:") {
				t.Fatalf("incorrect summary: %s", &out)
			}
			if err != nil {
				t.Fatalf("missing body should be ignored: %v", err)
			}
			if !strings.Contains(warnings.String(), "UID 7") {
				t.Fatalf("missing failed identity: %v", err)
			}
			if mode != "removed" && !strings.Contains(warnings.String(), "RFC822.SIZE=145300797") {
				t.Fatalf("missing server size: %v", err)
			}
			for _, rel := range []string{"SU5CT1g/42/8.eml", "T3RoZXI/42/9.eml"} {
				if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "SU5CT1g/42/7.eml")); !os.IsNotExist(err) {
				t.Fatal("published missing body")
			}
			marker := ignoredPath(filepath.Join(dir, "SU5CT1g", "42"), 7)
			if mode == "removed" {
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("cached a disappeared UID")
				}
			}
			if mode == "no_literal" {
				info, err := os.Stat(marker)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("record permissions: %v", err)
				}
				out.Reset()
				warnings.Reset()
				if err := syncMailboxes(c, dir, []string{"INBOX", "Other"}, time.Time{}, &out, &warnings); err != nil {
					t.Fatal(err)
				}
				if bodyRequests.Load() != 1 || warnings.Len() != 0 || !strings.Contains(out.String(), "Complete: downloaded=0 ignored=1") {
					t.Fatalf("repeated ignored request or warning: %s %s", &out, &warnings)
				}
				// A reused UID in a new UIDVALIDITY must not inherit the old record.
				validity.Store(43)
				out.Reset()
				warnings.Reset()
				if err := syncMailboxes(c, dir, []string{"INBOX"}, time.Time{}, &out, &warnings); err != nil {
					t.Fatal(err)
				}
				if bodyRequests.Load() != 2 || warnings.Len() == 0 {
					t.Fatal("UIDVALIDITY change reused ignored state")
				}
				validity.Store(42)
				recovered.Store(true)
				out.Reset()
				warnings.Reset()
				if err := syncMailboxes(c, dir, []string{"INBOX"}, time.Time{}, &out, &warnings, imapSyncOptions{retryIgnored: true}); err != nil {
					t.Fatal(err)
				}
				if bodyRequests.Load() != 3 || warnings.Len() != 0 || !strings.Contains(out.String(), "downloaded=1 existing=1") {
					t.Fatalf("retry did not recover: %s %s", &out, &warnings)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("successful retry kept ignored record")
				}
				if _, err := os.Stat(filepath.Join(dir, "SU5CT1g", "42", "7.eml")); err != nil {
					t.Fatal("recovered mail missing")
				}
			}
			_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					t.Error(err)
					return err
				}
				if strings.HasPrefix(d.Name(), ".download-") {
					t.Error("partial file leaked")
				}
				return nil
			})
		})
	}
}

func TestCachedIgnoredMailRespectsExactCutoff(t *testing.T) {
	cutoff := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name         string
		received     time.Time
		retry        bool
		wantFiltered int
		wantIgnored  int
		wantError    bool
	}{
		{name: "before_cutoff", received: cutoff.Add(-time.Second), wantFiltered: 1},
		{name: "inclusive_boundary", received: cutoff, wantIgnored: 1},
		{name: "recent_with_timezone", received: cutoff.Add(time.Hour).In(time.FixedZone("west", -12*3600)), wantIgnored: 1},
		{name: "missing_internaldate", wantError: true},
		{name: "retry_before_cutoff", received: cutoff.Add(-time.Second), retry: true, wantFiltered: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "SU5CT1g", "42")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := rememberIgnored(dir, &unavailableMessageError{uid: 7, size: 145300797}); err != nil {
				t.Fatal(err)
			}
			c, done := testPeerUID(t, 42, false, 7, tc.received)
			var output bytes.Buffer
			progress := &syncProgress{out: &output, started: time.Now()}
			downloaded, existing, err := syncFolder(c, root, "INBOX", cutoff, imapSyncOptions{progress: progress, retryIgnored: tc.retry})
			var incomplete *incompleteFolderError
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "omitted INTERNALDATE") {
					t.Fatalf("expected missing date error, got %v", err)
				}
			} else if tc.wantIgnored > 0 {
				if !errors.As(err, &incomplete) || incomplete.cached != tc.wantIgnored || len(incomplete.failures) != 0 {
					t.Fatalf("expected cached ignored mail, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if downloaded != 0 || existing != 0 || progress.filtered != tc.wantFiltered || progress.ignored != tc.wantIgnored {
				t.Fatalf("downloaded=%d existing=%d filtered=%d ignored=%d", downloaded, existing, progress.filtered, progress.ignored)
			}
			_ = c.Close()
			if got := <-done; got != 0 {
				t.Fatalf("unexpected body fetches: %d", got)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != ".ignored-7.json" {
				t.Fatalf("ignored record should be preserved without publishing mail: entries=%v err=%v", entries, err)
			}
		})
	}
}
