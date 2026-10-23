package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsAndConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := Load("")
	if err != nil || cfg.Host != DefaultIMAPHost || cfg.Protocol != "imap" || cfg.SMTP.Host != "mail.hust.edu.cn" || cfg.SMTP.Port != 465 || cfg.Timeout != "30m" || cfg.Dir != "~/HUST-Mail/" {
		t.Fatal("default config failed")
	}
	path := filepath.Join(home, ".config", "hust-mail-cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"user":"test@hust.edu.cn","password":"dedicated","port":1993,"protocol":"imap"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load("")
	if err != nil || cfg.Password != "dedicated" || cfg.User != "test@hust.edu.cn" || cfg.Port != 1993 || cfg.Protocol != "imap" {
		t.Fatal("config not loaded")
	}
	if _, err := Load(filepath.Join(home, "missing.json")); err == nil {
		t.Fatal("missing explicit config accepted")
	}
}

func TestExampleServerSettings(t *testing.T) {
	raw, err := os.ReadFile("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Protocol != "imap" || cfg.Host != "imap.hust.edu.cn" || cfg.Port != 993 || cfg.SMTP.Host != "mail.hust.edu.cn" || cfg.SMTP.Port != 465 {
		t.Fatal("example must prefill HUST TLS servers")
	}
}

func TestExpandArchiveDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for path, want := range map[string]string{
		"~/HUST-Mail/": filepath.Join(home, "HUST-Mail"),
		"~/other-mail": filepath.Join(home, "other-mail"),
		"~":            home,
		"./archive":    "./archive",
		"/tmp/archive": "/tmp/archive",
	} {
		got, err := ExpandDir(path)
		if err != nil || got != want {
			t.Errorf("ExpandDir(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestMalformedConfigNeverLeaksValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, raw := range []string{`null`, `[]`, `{"password":"secret-marker","typo":"secret-marker"}`, `{"port":"secret-marker"}`, `{"password":secret-marker}`, `{} {}`} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(path)
		if err == nil {
			t.Fatalf("accepted malformed config")
		}
		if strings.Contains(err.Error(), "secret-marker") {
			t.Fatal("secret leaked in error")
		}
	}
}

func TestCredentialFilePermissions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mode      os.FileMode
		password  string
		wantError bool
	}{
		{"owner_read_write", 0600, "secret-marker", false},
		{"owner_read_only", 0400, "secret-marker", false},
		{"group_read", 0640, "secret-marker", true},
		{"other_read", 0604, "secret-marker", true},
		{"world_readable", 0644, "secret-marker", true},
		{"other_execute", 0601, "secret-marker", true},
		{"passwordless_shared_config", 0644, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(`{"password":"`+tc.password+`"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if !tc.wantError {
				if err != nil || cfg.Password != tc.password {
					t.Fatal("safe config rejected")
				}
				return
			}
			if err == nil {
				t.Fatal("insecure credential file accepted")
			}
			if cfg.Password != "" || strings.Contains(err.Error(), tc.password) {
				t.Fatal("credential exposed on failure")
			}
			if !strings.Contains(err.Error(), "chmod 600") {
				t.Fatal("missing permission repair instructions")
			}
		})
	}
}
