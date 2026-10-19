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
	abs, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	// Create parents as needed; the archive itself must be private and not a symlink.
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return "", err
	}
	if err := privateDir(abs); err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(host + "\x00" + user))
	root := filepath.Join(abs, fmt.Sprintf("%x", hash[:]))
	return root, privateDir(root)
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

func lockArchive(root string) (func(), error) {
	path := filepath.Join(root, ".sync.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("archive locked or inaccessible (%s); see README for stale-lock recovery: %w", path, err)
	}
	if _, err := fmt.Fprintln(f, os.Getpid()); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
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
