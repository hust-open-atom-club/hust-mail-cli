package mailarchive

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func popPeer(t *testing.T, truncated bool) (net.Conn, <-chan int) {
	t.Helper()
	a, b := net.Pipe()
	a.SetDeadline(time.Now().Add(5 * time.Second))
	b.SetDeadline(time.Now().Add(5 * time.Second))
	done := make(chan int, 1)
	go func() {
		count := 0
		defer func() { b.Close(); done <- count }()
		fmt.Fprint(b, "+OK ready\r\n")
		scanner := bufio.NewScanner(b)
		for scanner.Scan() {
			switch line := scanner.Text(); line {
			case "USER user@hust.edu.cn", "PASS dedicated-password":
				fmt.Fprint(b, "+OK\r\n")
			case "UIDL":
				fmt.Fprint(b, "+OK\r\n1 stable-id\r\n.\r\n")
			case "RETR 1":
				count++
				fmt.Fprint(b, "+OK\r\nSubject: test\r\n\r\n..dot\r\n你好\r\n")
				if truncated {
					return
				}
				fmt.Fprint(b, ".\r\n")
			case "QUIT":
				fmt.Fprint(b, "+OK\r\n")
				return
			default:
				t.Errorf("command outside POP3 receive allowlist: %q", line)
				return
			}
		}
	}()
	t.Cleanup(func() { a.Close() })
	return a, done
}

func TestPOP3ReadOnlyIncremental(t *testing.T) {
	dest := t.TempDir()
	for i, want := range []int{1, 0} {
		conn, done := popPeer(t, false)
		if err := syncPOP3(conn, dest, "test:995", "user@hust.edu.cn", "dedicated-password", io.Discard); err != nil {
			t.Fatal(err)
		}
		conn.Close()
		if n := <-done; n != want {
			t.Fatalf("round %d downloads=%d", i, n)
		}
	}
	var paths []string
	filepath.WalkDir(dest, func(path string, d os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, ".eml") {
			paths = append(paths, path)
		}
		return err
	})
	if len(paths) != 1 {
		t.Fatalf("messages: %v", paths)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil || string(data) != "Subject: test\r\n\r\n.dot\r\n你好\r\n" {
		t.Fatalf("message=%q err=%v", data, err)
	}
}

func TestPOP3InterruptedDownload(t *testing.T) {
	dest := t.TempDir()
	conn, done := popPeer(t, true)
	if err := syncPOP3(conn, dest, "test:995", "user@hust.edu.cn", "dedicated-password", io.Discard); err == nil {
		t.Fatal("accepted truncated message")
	}
	conn.Close()
	<-done
	filepath.WalkDir(dest, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if !d.IsDir() && d.Name() != ".sync.flock" {
			t.Errorf("partial file leaked: %s", path)
		}
		return nil
	})
}
