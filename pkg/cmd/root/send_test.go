package root

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/mailsend"
)

func TestSendConfigFlagsStdinAndJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HUST_MAIL_USER", "env@example.com")
	t.Setenv("HUST_MAIL_PASSWORD", "env-secret")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"user":"config@example.com","password":"config-secret","timeout":"20s"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, flags := range []bool{false, true} {
		var out bytes.Buffer
		root := NewCmdRoot(&out, io.Discard)
		old, _, _ := root.Find([]string{"send"})
		root.RemoveCommand(old)
		called := false
		root.AddCommand(newSendCommandWithSender(func(ctx context.Context, o mailsend.Options) (mailsend.Result, error) {
			called = true
			user, host, port, timeout := "env@example.com", "mail.hust.edu.cn", 465, 20*time.Second
			if flags {
				user, host, port, timeout = "flag@example.com", "mail.hust.edu.cn", 465, 3*time.Second
			}
			if o.User != user || o.Password != "env-secret" || o.Host != host || o.Port != port || o.Timeout != timeout || o.Body != "来自标准输入\n" || len(o.To) != 2 {
				t.Fatalf("wrong send options (credentials omitted)")
			}
			return mailsend.Result{Status: "submitted", MessageID: "<test@example.com>", Recipients: 2}, nil
		}))
		root.SetIn(strings.NewReader("来自标准输入\n"))
		args := []string{"--config", path, "--json", "send", "--to", "one@example.com", "--to", "two@example.com", "--subject", "测试", "--body-file", "-"}
		if flags {
			args = append(args, "--user", "flag@example.com", "--timeout", "3s")
		}
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		var result mailsend.Result
		if err := json.Unmarshal(out.Bytes(), &result); err != nil || !called || result.Status != "submitted" {
			t.Fatalf("bad output: %s %v", &out, err)
		}
	}
}

func TestSendRequiresExplicitMessageAndBodySource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{
		{"send"},
		{"send", "--to", "to@example.com", "--subject", "test"},
		{"send", "--to", "to@example.com", "--subject", "test", "--body", "text", "--body-file", "-"},
		{"send", "extra", "--to", "to@example.com", "--subject", "test", "--body", "text"},
	} {
		cmd := NewCmdRoot(io.Discard, io.Discard)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestSendTimeoutOverridesMalformedConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HUST_MAIL_USER", "sender@example.com")
	t.Setenv("HUST_MAIL_PASSWORD", "fixture-secret")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"timeout":"bad"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, override := range []bool{false, true} {
		root := NewCmdRoot(io.Discard, io.Discard)
		old, _, _ := root.Find([]string{"send"})
		root.RemoveCommand(old)
		called := false
		root.AddCommand(newSendCommandWithSender(func(ctx context.Context, o mailsend.Options) (mailsend.Result, error) {
			called = true
			if o.Timeout != time.Second {
				t.Fatalf("got timeout %v, want 1s", o.Timeout)
			}
			return mailsend.Result{Status: "submitted"}, mailsend.Validate(o)
		}))
		args := []string{"send", "--config", path, "--to", "to@example.com", "--subject", "test", "--body", "text"}
		if override {
			args = append(args, "--timeout", "1s")
		}
		root.SetArgs(args)
		err := root.Execute()
		if override && (err != nil || !called) {
			t.Fatalf("explicit timeout did not override malformed config: %v", err)
		}
		if !override && (err == nil || called) {
			t.Fatal("malformed config timeout was not rejected before submission")
		}
	}
}

// Signal handling cancels the command context, even when stdin has not reached
// EOF. A producer must not have to close its pipe for the CLI to exit.
func TestSendStdinCancellationDoesNotSubmit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := NewCmdRoot(io.Discard, io.Discard)
	old, _, _ := root.Find([]string{"send"})
	root.RemoveCommand(old)
	submitted := false
	root.AddCommand(newSendCommandWithSender(func(context.Context, mailsend.Options) (mailsend.Result, error) {
		submitted = true
		return mailsend.Result{}, nil
	}))
	root.SetIn(&startedBodyReader{Reader: reader, started: started})
	root.SetArgs([]string{"send", "--to", "to@example.com", "--subject", "test", "--body-file", "-"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("command did not start reading stdin")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		writer.Close()
		<-done
		t.Fatal("canceled command is still waiting for stdin EOF")
	}
	if submitted {
		t.Fatal("submitted mail after input was canceled")
	}
	// The command must not close its caller-owned input on cancellation.
	if err := writer.CloseWithError(io.ErrUnexpectedEOF); err != nil {
		t.Fatal(err)
	}
}

type startedBodyReader struct {
	io.Reader
	started chan struct{}
}

func (r *startedBodyReader) Read(p []byte) (int, error) {
	if r.started != nil {
		close(r.started)
		r.started = nil
	}
	return r.Reader.Read(p)
}
