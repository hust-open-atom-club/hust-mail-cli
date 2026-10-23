package root

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/mailsend"
	"github.com/spf13/cobra"
)

type fullOutputWriter struct{}

func (fullOutputWriter) Write([]byte) (int, error) { return 0, syscall.ENOSPC }

func sendOutputRoot(out, stderr io.Writer, sendErr error) (*cobra.Command, *int) {
	root := NewCmdRoot(out, stderr)
	old, _, _ := root.Find([]string{"send"})
	root.RemoveCommand(old)
	calls := new(int)
	root.AddCommand(newSendCommandWithSender(func(context.Context, mailsend.Options) (mailsend.Result, error) {
		*calls++
		return mailsend.Result{Status: "submitted", MessageID: "<accepted@example.com>", Recipients: 1}, sendErr
	}))
	return root, calls
}

func TestSendPreservesAcceptanceAfterOutputFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, jsonMode := range []bool{false, true} {
		for _, brokenWarnings := range []bool{false, true} {
			var warnings bytes.Buffer
			var stderr io.Writer = &warnings
			if brokenWarnings {
				stderr = fullOutputWriter{}
			}
			root, calls := sendOutputRoot(fullOutputWriter{}, stderr, nil)
			args := []string{"send", "--to", "recipient@example.com", "--subject", "test", "--body", "test"}
			if jsonMode {
				args = append(args, "--json")
			}
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatalf("accepted submission became an error: %v", err)
			}
			if *calls != 1 {
				t.Fatalf("submitted %d times", *calls)
			}
			if !brokenWarnings && (!strings.Contains(warnings.String(), "already accepted") || !strings.Contains(warnings.String(), "<accepted@example.com>")) {
				t.Fatalf("missing acceptance warning: %s", &warnings)
			}
		}
	}
}

func TestSendStillReturnsSubmissionFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	want := errors.New("SMTP submission rejected")
	var out, warnings bytes.Buffer
	root, calls := sendOutputRoot(&out, &warnings, want)
	root.SetArgs([]string{"send", "--to", "recipient@example.com", "--subject", "test", "--body", "test"})
	if err := root.Execute(); !errors.Is(err, want) {
		t.Fatalf("got %v, want submission error", err)
	}
	if *calls != 1 || out.Len() != 0 || warnings.Len() != 0 {
		t.Fatal("failed submission reported as accepted")
	}
}

func TestSendBrokenStdoutPreservesProcessSuccess(t *testing.T) {
	for _, mode := range []string{"text", "json"} {
		t.Run(mode, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			reader.Close()
			defer writer.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSendBrokenStdoutHelper$")
			cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "HUST_MAIL_SEND_OUTPUT_HELPER="+mode)
			cmd.Stdout = writer
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("accepted submission exited unsuccessfully on broken pipe: %v %s", err, &stderr)
			}
			if !strings.Contains(stderr.String(), "already accepted") {
				t.Fatalf("missing acceptance warning: %s", &stderr)
			}
		})
	}
}

func TestSendBrokenStdoutHelper(t *testing.T) {
	mode := os.Getenv("HUST_MAIL_SEND_OUTPUT_HELPER")
	if mode == "" {
		return
	}
	root, _ := sendOutputRoot(os.Stdout, os.Stderr, nil)
	args := []string{"send", "--to", "recipient@example.com", "--subject", "test", "--body", "test"}
	if mode == "json" {
		args = append(args, "--json")
	}
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
