package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCreatePrivateMinimalConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(dir, "config.json")
	cfg := Config{User: "test@hust.edu.cn", Password: "test-secret", Dir: DefaultArchiveDir}
	if err := Create(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.User != cfg.User || loaded.Password != cfg.Password || loaded.Dir != cfg.Dir || loaded.Timeout != "30m" {
		t.Fatal("incorrect created config", err)
	}
	for path, want := range map[string]os.FileMode{path: 0600, dir: 0700} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("permissions: %s %v", path, err)
		}
	}
	before, _ := os.ReadFile(path)
	if err := Create(path, Config{}); err == nil {
		t.Fatal("overwrote config")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("changed existing config")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("left temporary files")
	}
}

func TestCreateDoesNotFollowTargetSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "original")
	if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := Create(path, Config{}); err == nil {
		t.Fatal("accepted existing symlink")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "preserve" {
		t.Fatal("modified symlink target")
	}
}

func TestInitializedPathsCanBeLoadedWithSameArgument(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, arg := range []string{"", "~/custom-mail/config.json", filepath.Join(t.TempDir(), "config.json")} {
		path, err := ResolvePath(arg)
		if err != nil {
			t.Fatal(err)
		}
		want := Config{User: "test@hust.edu.cn", Password: "test-only-secret", Dir: DefaultArchiveDir}
		if err := Create(path, want); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(arg)
		if err != nil {
			t.Fatalf("cannot load initialized path %q: %v", arg, err)
		}
		if loaded.User != want.User || loaded.Password != want.Password || loaded.Dir != want.Dir {
			t.Fatalf("configuration did not round trip for %q", arg)
		}
	}
	if _, err := Load("~/missing-config.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing explicit config must remain an error: %v", err)
	}
}
