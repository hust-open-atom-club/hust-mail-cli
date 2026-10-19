package mailarchive

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
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
						fmt.Fprintf(b, "* 2 EXISTS\r\n* OK [UIDVALIDITY 42] valid\r\n%s OK [READ-ONLY] done\r\n", tag)
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
