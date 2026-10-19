package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const DefaultArchiveDir = "~/HUST-Mail/"

// ExpandDir resolves a leading ~/ after configuration and flag overrides.
// Relative paths remain relative to the command's working directory.
func ExpandDir(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

type Config struct {
	User     string `json:"user"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	Dir      string `json:"dir"`
	Mailbox  string `json:"mailbox"`
	Timeout  string `json:"timeout"`
}

// Load reads ~/.config explicitly, including on macOS where UserConfigDir
// would otherwise select ~/Library/Application Support.
// A missing default file is allowed; a missing explicit file is an error.
func Load(path string) (Config, error) {
	cfg := Config{Host: "mail.hust.edu.cn", Protocol: "imap", Dir: DefaultArchiveDir, Timeout: "30m"}
	explicit := path != ""
	if !explicit {
		home, err := os.UserHomeDir()
		if err != nil {
			return cfg, err
		}
		path = filepath.Join(home, ".config", "hust-mail-cli", "config.json")
	}
	f, err := os.Open(path)
	if !explicit && errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	// Do not include decoder errors: they can quote secret field values.
	parsed := &cfg
	if err := dec.Decode(&parsed); err != nil || parsed == nil {
		return Config{}, errors.New("invalid config JSON or unknown field")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Config{}, errors.New("config must contain one JSON object")
	}
	return cfg, nil
}
