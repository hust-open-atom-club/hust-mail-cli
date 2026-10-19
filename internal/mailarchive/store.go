package mailarchive

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func accountDir(dest, host, user string) (string, error) {
	root, err := accountPath(dest, host, user)
	if err != nil {
		return "", err
	}
	abs := filepath.Dir(root)
	// Create parents as needed; the archive itself must be private and not a symlink.
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return "", err
	}
	if err := privateDir(abs); err != nil {
		return "", err
	}
	return root, privateDir(root)
}

func accountPath(dest, host, user string) (string, error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(host + "\x00" + user))
	root := filepath.Join(abs, fmt.Sprintf("%x", hash[:]))
	return root, nil
}

func privateDir(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("not a real directory: %s", path)
	}
	return os.Chmod(path, 0700)
}

func atomicWrite(path string, write func(io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := write(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
