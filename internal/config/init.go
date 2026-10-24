package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// InitPath resolves the same default location as Load, without reading credentials.
func InitPath(path string) (string, error) {
	if path != "" {
		return ExpandDir(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "hust-mail-cli", "config.json"), nil
}

// Create publishes a complete, private minimal config without overwriting an
// existing file (including symlinks). Only newly created directories get 0700.
func Create(path string, cfg Config) error {
	data, err := json.MarshalIndent(struct {
		User     string `json:"user"`
		Password string `json:"password"`
		Dir      string `json:"dir"`
	}{cfg.User, cfg.Password, cfg.Dir}, "", "  ")
	if err != nil {
		return errors.New("cannot encode config")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".config-init-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(append(data, '\n')); err != nil {
		return errors.New("cannot write config")
	}
	if err = f.Sync(); err != nil {
		return errors.New("cannot sync config")
	}
	if err = f.Close(); err != nil {
		return errors.New("cannot close config")
	}
	// Link is atomic and fails if another init has already created the target.
	if err = os.Link(f.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("config already exists; edit it manually (not overwritten)")
		}
		return fmt.Errorf("publish config: %w", err)
	}
	return nil
}
