package mailarchive

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
)

// This peer rejects every command outside the receiving allowlist and asserts
// the actual wire form of EXAMINE and BODY.PEEK, rather than mocking Go calls.
func testPeer(t *testing.T, validity int, fail bool) (*imapclient.Client, <-chan int) {
	return testPeerUID(t, validity, fail, 7)
}

func testPeerUID(t *testing.T, validity int, fail bool, bodyUID int, received ...time.Time) (*imapclient.Client, <-chan int) {
	t.Helper()
	a, b := net.Pipe()
	_ = a.SetDeadline(time.Now().Add(10 * time.Second))
	_ = b.SetDeadline(time.Now().Add(10 * time.Second))
	done := make(chan int, 1)
	go func() {
		defer b.Close()
		fetches := 0
		defer func() { done <- fetches }()
		fmt.Fprint(b, "* PREAUTH [CAPABILITY IMAP4rev1] ready\r\n")
		r := bufio.NewScanner(b)
		for r.Scan() {
			line := r.Text()
			tag, command, _ := strings.Cut(line, " ")
			switch {
			case command == "CAPABILITY":
				fmt.Fprintf(b, "* CAPABILITY IMAP4rev1\r\n%s OK done\r\n", tag)
			case strings.HasPrefix(command, "LIST "):
				fmt.Fprintf(b, "* LIST () \"/\" INBOX\r\n* LIST (\\Noselect) \"/\" Parent\r\n%s OK done\r\n", tag)
			case command == "EXAMINE INBOX" || command == `EXAMINE "INBOX"`:
				fmt.Fprintf(b, "* 1 EXISTS\r\n* OK [UIDVALIDITY %d] valid\r\n%s OK [READ-ONLY] done\r\n", validity, tag)
			case command == "UID SEARCH ALL":
				if len(received) > 0 {
					t.Error("time-filtered sync searched all time")
					return
				}
				fmt.Fprintf(b, "* SEARCH 7\r\n%s OK done\r\n", tag)
			case strings.HasPrefix(command, "UID SEARCH SINCE "):
				if len(received) == 0 || !strings.Contains(command, "11-Oct-2026") {
					t.Errorf("unexpected time search: %s", command)
					return
				}
				fmt.Fprintf(b, "* SEARCH 7\r\n%s OK done\r\n", tag)
			case command == "UID FETCH 7 (UID)":
				fmt.Fprintf(b, "* 1 FETCH (UID 7)\r\n%s OK done\r\n", tag)
			case command == "UID FETCH 7 (UID INTERNALDATE)":
				if len(received) == 0 {
					t.Error("unexpected INTERNALDATE request")
					return
				}
				if received[0].IsZero() {
					fmt.Fprintf(b, "* 1 FETCH (UID 7)\r\n%s OK done\r\n", tag)
				} else {
					fmt.Fprintf(b, "* 1 FETCH (UID 7 INTERNALDATE %q)\r\n%s OK done\r\n", received[0].Format("02-Jan-2006 15:04:05 -0700"), tag)
				}
			case strings.HasPrefix(command, "FETCH "):
				if !strings.Contains(command, "BODY.PEEK[]") || !strings.HasPrefix(command, "FETCH 1 ") {
					t.Errorf("unsafe or unexpected fetch: %s", command)
					return
				}
				fetches++
				body := "Subject: test\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n你好\r\n"
				fmt.Fprintf(b, "* 1 FETCH (BODY[] {%d}\r\n%s UID %d)\r\n", len(body), body, bodyUID)
				if fail {
					fmt.Fprintf(b, "%s NO download failed\r\n", tag)
				} else {
					fmt.Fprintf(b, "%s OK done\r\n", tag)
				}
			default:
				t.Errorf("command outside receiving allowlist: %s", command)
				return
			}
		}
	}()
	c := imapclient.New(a, nil)
	t.Cleanup(func() { _ = c.Close() })
	return c, done
}

func TestReceiveTimeCutoff(t *testing.T) {
	cutoff := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		received  time.Time
		wantFetch int
		wantError bool
	}{
		{"old", cutoff.Add(-time.Second), 0, false},
		{"inclusive_boundary", cutoff, 1, false},
		{"recent_with_timezone", cutoff.Add(time.Hour).In(time.FixedZone("west", -12*3600)), 1, false},
		{"missing_internaldate", time.Time{}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			c, done := testPeerUID(t, 42, false, 7, tc.received)
			n, _, err := syncFolder(c, root, "INBOX", cutoff)
			if (err != nil) != tc.wantError || n != tc.wantFetch {
				t.Fatalf("downloaded=%d err=%v", n, err)
			}
			c.Close()
			if got := <-done; got != tc.wantFetch {
				t.Fatalf("BODY.PEEK fetches=%d; want %d", got, tc.wantFetch)
			}
			entries, err := os.ReadDir(filepath.Join(root, "SU5CT1g", "42"))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != tc.wantFetch {
				t.Fatal("old or failed fetch left a local file")
			}
		})
	}
}

func TestReadOnlyIncrementalAndUIDValidity(t *testing.T) {
	root := t.TempDir()
	c, done := testPeer(t, 42, false)
	boxes, err := listFolders(c)
	if err != nil || len(boxes) != 1 || boxes[0] != "INBOX" {
		t.Fatalf("folders: %v %v", boxes, err)
	}
	for i, want := range []int{1, 0} {
		n, skipped, err := syncFolder(c, root, "INBOX", time.Time{})
		if err != nil || n != want || skipped != i {
			t.Fatalf("round %d: %d %d %v", i, n, skipped, err)
		}
	}
	path := filepath.Join(root, "SU5CT1g", "42", "7.eml")
	body, err := os.ReadFile(path)
	if err != nil || !strings.HasSuffix(string(body), "你好\r\n") {
		t.Fatalf("raw bytes: %q %v", body, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
	// A locally missing message must be fetched again even if higher UIDs existed.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if n, _, err := syncFolder(c, root, "INBOX", time.Time{}); err != nil || n != 1 {
		t.Fatalf("restore: %d %v", n, err)
	}
	c.Close()
	if n := <-done; n != 2 {
		t.Fatalf("fetches: %d", n)
	}
	c2, done2 := testPeer(t, 43, false)
	if n, _, err := syncFolder(c2, root, "INBOX", time.Time{}); err != nil || n != 1 {
		t.Fatalf("new validity: %d %v", n, err)
	}
	for _, v := range []int{42, 43} {
		if _, err := os.Stat(filepath.Join(root, "SU5CT1g", strconv.Itoa(v), "7.eml")); err != nil {
			t.Fatal(err)
		}
	}
	c2.Close()
	<-done2
}

func TestFailedFetchNeverPublishesMessage(t *testing.T) {
	root := t.TempDir()
	c, done := testPeer(t, 42, true)
	if _, _, err := syncFolder(c, root, "INBOX", time.Time{}); err == nil {
		t.Fatal("expected tagged NO failure")
	}
	entries, err := os.ReadDir(filepath.Join(root, "SU5CT1g", "42"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial file leaked: %v %v", entries, err)
	}
	c.Close()
	<-done
}

func TestSequenceShiftNeverArchivesWrongUID(t *testing.T) {
	root := t.TempDir()
	c, done := testPeerUID(t, 42, false, 8)
	if _, _, err := syncFolder(c, root, "INBOX", time.Time{}); err == nil {
		t.Fatal("accepted another message after sequence shift")
	}
	entries, err := os.ReadDir(filepath.Join(root, "SU5CT1g", "42"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("wrong message published: %v %v", entries, err)
	}
	c.Close()
	<-done
}

func TestArchiveIsolationLockAndAtomicFailure(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "archive")
	a, err := accountDir(dest, "mail.hust.edu.cn", "one@hust.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	b, err := accountDir(dest, "mail.hust.edu.cn", "two@hust.edu.cn")
	if err != nil || a == b {
		t.Fatal("accounts not isolated", err)
	}
	unlock, err := lockArchive(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockArchive(a); err == nil {
		t.Fatal("concurrent archive accepted")
	}
	unlock()
	path := filepath.Join(a, "message.eml")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	err = atomicWrite(path, func(w io.Writer) error { io.WriteString(w, "partial"); return errors.New("interrupted") })
	if err == nil {
		t.Fatal("expected error")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Fatal("failed write replaced archive")
	}
	link := filepath.Join(a, "link")
	if err := os.Symlink(b, link); err != nil {
		t.Fatal(err)
	}
	if err := privateDir(link); err == nil {
		t.Fatal("symlink accepted")
	}
}
