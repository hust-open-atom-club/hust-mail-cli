package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadProcessExitCodes(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, raw string
		want      int
	}{
		{"broken_multipart", "Subject: broken\r\nContent-Type: multipart/mixed\r\n\r\nbad", 1},
		{"broken_transfer", "Subject: broken\r\nContent-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\n!!!!", 1},
		{"recoverable_charset", "Subject: good\r\nContent-Type: text/plain; charset=y\r\n\r\n中文 BPF", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".eml")
			if err := os.WriteFile(path, []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestCLIExitHelper$")
			cmd.Env = append(os.Environ(), "HUST_MAIL_CLI_EXIT_HELPER=1", "HUST_MAIL_CLI_EXIT_FILE="+path)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err := cmd.Run()
			code := 0
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					code = exit.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if code != tc.want {
				t.Fatalf("exit=%d want=%d stderr=%s", code, tc.want, &stderr)
			}
			if tc.want != 0 && !strings.HasPrefix(stderr.String(), "error:") {
				t.Fatalf("missing CLI error: %s", &stderr)
			}
		})
	}
}

func TestCLIExitHelper(t *testing.T) {
	if os.Getenv("HUST_MAIL_CLI_EXIT_HELPER") != "1" {
		return
	}
	os.Args = []string{"hust-mail-cli", "read", os.Getenv("HUST_MAIL_CLI_EXIT_FILE")}
	code := run()
	if code != 0 {
		os.Exit(code)
	}
}
