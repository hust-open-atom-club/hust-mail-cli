//go:build linux || darwin

package mailarchive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func writeLockFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveRefusesActiveAndUnknownLocks(t *testing.T) {
	for _, kind := range []string{"kernel", "legacy", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if kind == "kernel" {
				unlock, err := lockArchive(root)
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
			} else {
				text := strconv.Itoa(os.Getpid())
				if kind == "unknown" {
					text = "invalid PID"
				}
				writeLockFile(t, filepath.Join(root, ".sync.lock"), text)
			}
			partial := filepath.Join(root, ".download-active")
			writeLockFile(t, partial, "keep")
			if _, err := lockArchive(root); err == nil {
				t.Fatal("active or unknown lock bypassed")
			}
			if _, err := os.Stat(partial); err != nil {
				t.Fatal("active partial removed")
			}
		})
	}
}

func TestArchiveLockHelper(t *testing.T) {
	root := os.Getenv("HUST_MAIL_TEST_LOCK_ROOT")
	if root == "" {
		return
	}
	unlock, err := lockArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	fmt.Fprintln(os.Stdout, "locked")
	for {
		time.Sleep(time.Hour)
	}
}

func TestKilledProcessLockIsRecovered(t *testing.T) {
	for _, signal := range []unix.Signal{unix.SIGKILL, unix.SIGHUP} {
		t.Run(signal.String(), func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestArchiveLockHelper$")
			cmd.Env = append(os.Environ(), "HUST_MAIL_TEST_LOCK_ROOT="+root)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			line, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil || line != "locked\n" {
				t.Fatalf("child startup %q %v", line, err)
			}
			if _, err := lockArchive(root); !errors.Is(err, errArchiveBusy) {
				t.Fatal("live lock not enforced", err)
			}
			if err := cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			if _, err := os.Stat(filepath.Join(root, ".sync.lock")); err != nil {
				t.Fatal("test needs abandoned marker", err)
			}
			unlock, err := lockArchive(root)
			if err != nil {
				t.Fatal("automatic recovery failed", err)
			}
			unlock()
			// Exercise compatibility with a dead PID marker from an old client.
			writeLockFile(t, filepath.Join(root, ".sync.lock"), strconv.Itoa(cmd.Process.Pid))
			unlock, err = lockArchive(root)
			if err != nil {
				t.Fatal("legacy recovery failed", err)
			}
			unlock()
		})
	}
}
