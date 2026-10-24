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

// HUST transport endpoints are fixed and are not user configuration.
const MailHost = "mail.hust.edu.cn"
const IMAPPort = 993
const SMTPPort = 465

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
	Dir      string `json:"dir"`
	Mailbox  string `json:"mailbox"`
	Days     int    `json:"days"`
	Timeout  string `json:"timeout"`
}

// Load reads ~/.config explicitly, including on macOS where UserConfigDir
// would otherwise select ~/Library/Application Support.
// A missing default file is allowed; a missing explicit file is an error.
func Load(path string) (Config, error) {
	cfg := Config{Dir: DefaultArchiveDir, Timeout: "30m"}
	explicit := path != ""
	path, err := ResolvePath(path)
	if err != nil {
		return cfg, err
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
	// Decode errors can quote credentials; never include their original text.
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return Config{}, errors.New("invalid config JSON or unknown field")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Config{}, errors.New("config must contain one JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Config{}, errors.New("invalid config JSON or unknown field")
	}
	var removed []string
	for _, key := range []string{"protocol", "host", "port", "smtp"} {
		if _, exists := fields[key]; exists {
			removed = append(removed, key)
		}
	}
	if len(removed) > 0 {
		return Config{}, fmt.Errorf("removed config fields: %s; delete these fields: HUST IMAP and SMTP use fixed TLS endpoints", strings.Join(removed, ", "))
	}
	for key := range fields {
		switch key {
		case "user", "password", "dir", "mailbox", "days", "timeout":
		default:
			return Config{}, errors.New("invalid config JSON or unknown field")
		}
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, errors.New("invalid config JSON or unknown field")
	}
	if cfg.Password != "" {
		// Inspect the opened descriptor, rather than re-statting the path.
		// Owner-only read access (0400) is also safe for credential loading.
		info, err := f.Stat()
		if err != nil {
			return Config{}, errors.New("cannot check credential config permissions")
		}
		if info.Mode().Perm()&0077 != 0 {
			return Config{}, errors.New("config containing password must have no group or other permissions; run chmod 600 on the config file")
		}
	}
	return cfg, nil
}
