//go:build linux || darwin

package mailarchive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

var errArchiveBusy = errors.New("archive is in use by another sync process")

// Never unlink .sync.flock: all new clients must lock the same inode. The
// kernel releases this lock even on SIGKILL. The PID marker retains exclusion
// with old clients, which use O_EXCL on .sync.lock instead of flock.
func kernelLock(root string) (*os.File, error) {
	flags := os.O_RDWR | os.O_CREATE | unix.O_NOFOLLOW
	f, err := os.OpenFile(filepath.Join(root, ".sync.flock"), flags, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("invalid archive kernel lock file")
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errArchiveBusy
		}
		return nil, fmt.Errorf("lock archive: %w", err)
	}
	return f, nil
}

// Called only while holding .sync.flock. Unknown markers are never removed.
func markerState(root string) (state string, pid int, err error) {
	f, err := os.OpenFile(filepath.Join(root, ".sync.lock"), os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return "absent", 0, nil
	}
	if err != nil {
		return "unknown", 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "unknown", 0, errors.New("invalid archive PID lock file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 129))
	if err != nil {
		return "unknown", 0, err
	}
	text := strings.TrimSpace(string(data))
	if strings.HasPrefix(text, "flock-v1 ") {
		pid, err = strconv.Atoi(strings.TrimPrefix(text, "flock-v1 "))
		if err != nil || pid <= 0 {
			return "unknown", 0, errors.New("unrecognized PID lock; inspect it before manual recovery")
		}
		return "stale", pid, nil // a current client would still hold the kernel lock
	}
	pid, err = strconv.Atoi(text)
	if err != nil || pid <= 0 {
		return "unknown", 0, errors.New("unrecognized PID lock; inspect it before manual recovery")
	}
	err = unix.Kill(pid, 0)
	if err == nil || errors.Is(err, unix.EPERM) {
		return "active", pid, nil
	}
	if errors.Is(err, unix.ESRCH) {
		return "stale", pid, nil
	}
	return "unknown", pid, errors.New("cannot determine whether the recorded process is alive")
}

func lockArchive(root string) (func(), error) {
	kernel, err := kernelLock(root)
	if err != nil {
		return nil, fmt.Errorf("archive unavailable: %w", err)
	}
	fail := func(err error) (func(), error) { kernel.Close(); return nil, err }
	state, _, err := markerState(root)
	if err != nil {
		return fail(err)
	}
	if state == "active" {
		return fail(errArchiveBusy)
	}
	path := filepath.Join(root, ".sync.lock")
	if state == "stale" {
		if err := os.Remove(path); err != nil {
			return fail(err)
		}
	}
	// Publish a complete marker atomically. A crash between create and write
	// must not leave an empty, unclassifiable marker that blocks future syncs.
	f, err := os.CreateTemp(root, ".lock-*")
	if err != nil {
		return fail(fmt.Errorf("create archive PID lock: %w", err))
	}
	defer os.Remove(f.Name())
	info, statErr := f.Stat()
	_, writeErr := fmt.Fprintf(f, "flock-v1 %d\n", os.Getpid())
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if statErr != nil || writeErr != nil || closeErr != nil {
		return fail(errors.Join(statErr, writeErr, closeErr))
	}
	if err := os.Link(f.Name(), path); err != nil {
		return fail(fmt.Errorf("publish archive PID lock: %w", err))
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if current, err := os.Lstat(path); err == nil && os.SameFile(info, current) {
				_ = os.Remove(path)
			}
			_ = kernel.Close()
		})
	}, nil
}
