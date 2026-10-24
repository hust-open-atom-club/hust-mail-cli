package root

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/mailcheck"
)

func TestDoctorConfigAndOutput(t *testing.T) {
	t.Setenv("HUST_MAIL_USER", "env@hust.edu.cn")
	t.Setenv("HUST_MAIL_PASSWORD", "env-secret")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"user":"config@hust.edu.cn","password":"config-secret","timeout":"9s"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, jsonMode := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			var out bytes.Buffer
			root := NewCmdRoot(&out, io.Discard)
			old, _, _ := root.Find([]string{"doctor"})
			root.RemoveCommand(old)
			root.AddCommand(newDoctorCommandWithChecker(func(ctx context.Context, o mailcheck.Options) ([]mailcheck.Result, error) {
				if o.User != "env@hust.edu.cn" || o.Password != "env-secret" || o.Timeout != 2*time.Second {
					t.Errorf("incorrect config resolution")
				}
				return []mailcheck.Result{{Protocol: "imap", Endpoint: "mail.hust.edu.cn:993", OK: true, Stage: "auth", Message: "ok"}, {Protocol: "smtp", Endpoint: "mail.hust.edu.cn:465", OK: !fail, Stage: "auth", Message: "check"}}, nil
			}))
			args := []string{"doctor", "--config", path, "--timeout", "2s"}
			if jsonMode {
				args = append(args, "--json")
			}
			root.SetArgs(args)
			err := root.Execute()
			if (err != nil) != fail {
				t.Fatalf("failure=%v err=%v", fail, err)
			}
			if strings.Contains(out.String(), "secret") {
				t.Fatal("credential leak")
			}
			if jsonMode {
				lines := strings.Split(strings.TrimSpace(out.String()), "\n")
				if len(lines) != 2 {
					t.Fatal(out.String())
				}
				for _, line := range lines {
					var r mailcheck.Result
					if err := json.Unmarshal([]byte(line), &r); err != nil {
						t.Fatal(err)
					}
				}
			} else if !strings.Contains(out.String(), "OK") || (fail && !strings.Contains(out.String(), "FAIL")) {
				t.Fatal(out.String())
			}
		}
	}
}

func TestDoctorRejectsInvalidConfigWithoutNetwork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HUST_MAIL_USER", "")
	t.Setenv("HUST_MAIL_PASSWORD", "")
	for _, args := range [][]string{{"doctor"}, {"doctor", "extra"}, {"doctor", "--timeout", "0"}, {"doctor", "--host", "example.com"}} {
		cmd := NewCmdRoot(io.Discard, io.Discard)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}
