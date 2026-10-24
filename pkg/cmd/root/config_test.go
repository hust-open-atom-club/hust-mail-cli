package root

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/config"
	"github.com/spf13/cobra"
)

func TestConfigInitCreatesWithoutExposingPassword(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonMode], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			var out bytes.Buffer
			root := NewCmdRoot(&out, io.Discard)
			old, _, _ := root.Find([]string{"config"})
			root.RemoveCommand(old)
			calls := 0
			root.AddCommand(newConfigCommandWithPrompt(func(context.Context, *cobra.Command) (config.Config, error) {
				calls++
				return config.Config{User: "test@hust.edu.cn", Password: "hidden-secret", Dir: config.DefaultArchiveDir}, nil
			}))
			args := []string{"config", "init"}
			if jsonMode {
				args = append(args, "--json")
			}
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "hidden-secret") {
				t.Fatal("password exposed")
			}
			path := filepath.Join(os.Getenv("HOME"), ".config", "hust-mail-cli", "config.json")
			if _, err := config.Load(path); err != nil {
				t.Fatal(err)
			}
			if jsonMode {
				var obj map[string]string
				if err := json.Unmarshal(out.Bytes(), &obj); err != nil || obj["status"] != "created" || obj["path"] != path {
					t.Fatal("invalid JSON output", err)
				}
			} else if !strings.Contains(out.String(), "days") || !strings.Contains(out.String(), "doctor") {
				t.Fatal("missing next steps")
			}
			root.SetArgs(args)
			if err := root.Execute(); err == nil || calls != 1 {
				t.Fatal("existing config prompted or overwritten")
			}
		})
	}
}

func TestConfigInitCancellationDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-created", "config.json")
	root := NewCmdRoot(io.Discard, io.Discard)
	old, _, _ := root.Find([]string{"config"})
	root.RemoveCommand(old)
	root.AddCommand(newConfigCommandWithPrompt(func(context.Context, *cobra.Command) (config.Config, error) {
		return config.Config{}, errors.New("canceled")
	}))
	root.SetArgs([]string{"config", "init", "--config", path})
	if err := root.Execute(); err == nil {
		t.Fatal("accepted canceled input")
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("created files before completed input")
	}
}

func TestTerminalInputCancellation(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	fd := int(r.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = (terminalIO{ctx: ctx, file: r, fd: fd}).Read(make([]byte, 1))
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatal("terminal input did not cancel", err)
	}
}
