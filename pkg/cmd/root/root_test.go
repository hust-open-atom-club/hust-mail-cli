package root

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/mailarchive"
	"github.com/spf13/cobra"
)

func TestCommandValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{
		{"doctor"}, {"diagnose"}, {"send"}, {"reply"}, {"forward"}, {"delete"}, {"smtp"},
		{"sync", "extra"}, {"read"}, {"list", "extra"},
		{"sync", "--protocol", "smtp"}, {"folders", "--protocol", "pop3"},
		{"sync", "--port", "-1"}, {"sync", "--password", "secret"},
		{"sync", "--days", "-1"}, {"sync", "--days", "36501"}, {"sync", "--protocol", "pop3", "--days", "7"},
		{"search"}, {"search", "query", "--limit", "-1"},
		{"attachments", "list"}, {"attachments", "export"},
	} {
		cmd := NewCmdRoot(io.Discard, io.Discard)
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(context.Background()); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}

func TestDaysConfigAndExplicitZeroOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"user":"test","days":7}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args           []string
		flagDays, want int
	}{
		{[]string{"sync", "--config", path}, 0, 7},
		{[]string{"sync", "--config", path, "--days", "0"}, 0, 0},
		{[]string{"sync", "--config", path, "--days", "3"}, 3, 3},
	} {
		cmd := NewCmdRoot(io.Discard, io.Discard)
		sync, _, _ := cmd.Find([]string{"sync"})
		sync.RunE = func(cmd *cobra.Command, args []string) error {
			opts, err := receiveOptions(cmd, mailarchive.Options{Days: tc.flagDays})
			if err == nil && opts.Days != tc.want {
				t.Errorf("days=%d; want %d", opts.Days, tc.want)
			}
			return err
		}
		cmd.SetArgs(tc.args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLocalMIMECommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "message.eml")
	raw := "Subject: =?utf-8?B?5L2g5aW9?=\r\nFrom: alice@example.com\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=test\r\n\r\n--test\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nHello world\r\n--test\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=hello.txt\r\nContent-Transfer-Encoding: base64\r\n\r\naGVsbG8=\r\n--test--\r\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "attachments")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"read", path}, "Subject: 你好"},
		{[]string{"read", "--raw", path}, "aGVsbG8="},
		{[]string{"search", "WORLD", "--dir", dir, "--from", "alice"}, `"subject":"你好"`},
		{[]string{"attachments", "list", path}, `"filename":"hello.txt"`},
		{[]string{"attachments", "export", path, "--index", "1", "--output", dest}, "001-hello.txt"},
	} {
		var out bytes.Buffer
		cmd := NewCmdRoot(&out, io.Discard)
		cmd.SetArgs(tc.args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("%v missing %q", tc.args, tc.want)
		}
	}
	data, err := os.ReadFile(filepath.Join(dest, "001-hello.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatal("exported data mismatch", err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != raw {
		t.Fatal("source message modified")
	}
}

func TestHelpAndLocalReadNeedNoCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HUST_MAIL_USER", "")
	t.Setenv("HUST_MAIL_PASSWORD", "")
	for _, args := range [][]string{{"--help"}, {"sync", "--help"}, {"folders", "--help"}, {"completion", "zsh"}} {
		cmd := NewCmdRoot(io.Discard, io.Discard)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "message.eml")
	raw := "Subject: =?UTF-8?B?5L2g5aW9?=\r\nFrom: test@example.com\r\n\r\nbody\r\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"list", "--dir", dir}, {"read", "--raw", path}} {
		var out bytes.Buffer
		cmd := NewCmdRoot(&out, io.Discard)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if args[0] == "read" && out.String() != raw {
			t.Fatalf("read changed message: %q", out.String())
		}
		if args[0] == "list" && !strings.Contains(out.String(), "你好") {
			t.Fatalf("undecoded subject: %s", out.String())
		}
	}
}

func TestConfigPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HUST_MAIL_USER", "env@example.com")
	t.Setenv("HUST_MAIL_PASSWORD", "env-password")
	path := filepath.Join(home, ".config", "hust-mail-cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"user":"config@example.com","password":"config-password","protocol":"pop3","port":995,"timeout":"2m","dir":"archive"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := NewCmdRoot(io.Discard, io.Discard)
	sync, _, err := cmd.Find([]string{"sync"})
	if err != nil {
		t.Fatal(err)
	}
	var got mailarchive.Options
	sync.RunE = func(cmd *cobra.Command, args []string) error {
		var err error
		got, err = receiveOptions(cmd, mailarchive.Options{User: "flag@example.com", Port: 0})
		return err
	}
	cmd.SetArgs([]string{"sync", "--user", "flag@example.com", "--port", "0"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got.User != "flag@example.com" || got.Password != "env-password" || got.Protocol != "pop3" || got.Port != 0 || got.Timeout != 2*time.Minute || got.Dir != "archive" {
		t.Fatal("configuration precedence failed")
	}
	if err := os.Unsetenv("HUST_MAIL_USER"); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("HUST_MAIL_PASSWORD"); err != nil {
		t.Fatal(err)
	}
	cmd = NewCmdRoot(io.Discard, io.Discard)
	sync, _, _ = cmd.Find([]string{"sync"})
	sync.RunE = func(cmd *cobra.Command, args []string) error {
		resolved, err := receiveOptions(cmd, mailarchive.Options{})
		if err != nil {
			return err
		}
		if resolved.User != "config@example.com" || resolved.Password != "config-password" {
			t.Fatal("config credentials not used")
		}
		return nil
	}
	cmd.SetArgs([]string{"sync"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	// Local listing also honors the configured archive directory.
	dir := t.TempDir()
	custom := filepath.Join(t.TempDir(), "custom.json")
	if err := os.WriteFile(custom, []byte(`{"dir":`+strconv.Quote(dir)+`}`), 0600); err != nil {
		t.Fatal(err)
	}
	cmd = NewCmdRoot(io.Discard, io.Discard)
	cmd.SetArgs([]string{"list", "--config", custom})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveDirectoryDefaultAndOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tc := range []struct {
		args          []string
		flagDir, want string
	}{
		{[]string{"sync"}, "", filepath.Join(home, "HUST-Mail")},
		{[]string{"sync", "--dir", "~/other-mail"}, "~/other-mail", filepath.Join(home, "other-mail")},
	} {
		cmd := NewCmdRoot(io.Discard, io.Discard)
		sync, _, _ := cmd.Find([]string{"sync"})
		sync.RunE = func(cmd *cobra.Command, args []string) error {
			opts, err := receiveOptions(cmd, mailarchive.Options{Dir: tc.flagDir})
			if err == nil && opts.Dir != tc.want {
				t.Errorf("archive = %q; want %q", opts.Dir, tc.want)
			}
			return err
		}
		cmd.SetArgs(tc.args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	// Local listing must resolve the same default and quoted tilde overrides.
	for _, dir := range []string{"HUST-Mail", "other-mail"} {
		if err := os.Mkdir(filepath.Join(home, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"list"}, {"list", "--dir", "~/other-mail"}} {
		cmd := NewCmdRoot(io.Discard, io.Discard)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	}
}
