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
	if err != nil || cfg.Host != "mail.hust.edu.cn" || cfg.Timeout != "30m" || cfg.Dir != "~/HUST-Mail/" {
		t.Fatal("default config failed")
	}
	path := filepath.Join(home, ".config", "hust-mail-cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"user":"test@hust.edu.cn","password":"dedicated","port":995,"protocol":"pop3"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load("")
	if err != nil || cfg.Password != "dedicated" || cfg.User != "test@hust.edu.cn" || cfg.Port != 995 || cfg.Protocol != "pop3" {
		t.Fatal("config not loaded")
	}
	if _, err := Load(filepath.Join(home, "missing.json")); err == nil {
		t.Fatal("missing explicit config accepted")
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
