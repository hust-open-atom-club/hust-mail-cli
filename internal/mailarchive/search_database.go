package mailarchive

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
	"golang.org/x/sys/unix"
)

var searchBucket = []byte("messages-v3")
var errCacheStructure = errors.New("invalid search database structure; use --rebuild-index")

// A disposable cache must not make a search fail on a corrupt database page.
func cacheAccess(fn func() error) (err error) {
	// bbolt reads mmap-backed pages with unsafe pointers. Ordinary recover
	// cannot catch a non-nil-address memory fault unless this goroutine opts in.
	previous := debug.SetPanicOnFault(true)
	defer debug.SetPanicOnFault(previous)
	defer func() {
		if recover() != nil {
			err = errCacheStructure
		}
	}()
	return fn()
}

// A separate persistent lock protects cache repair as well as normal use. A
// competing search falls back to source mail rather than waiting indefinitely.
func openPrivateCacheFile(path string, flags int, perm os.FileMode) (*os.File, error) {
	f, err := os.OpenFile(path, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK, perm)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1) {
		err = errors.New("cache file must be a regular file with one link")
	}
	if err == nil {
		err = f.Chmod(0600)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (s *searchIndex) openDatabase() (err error) {
	s.lock, err = openPrivateCacheFile(filepath.Join(s.dir, ".index.flock"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	if err = unix.Flock(int(s.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		s.lock.Close()
		s.lock = nil
		return errors.New("search cache busy; scanning source mail")
	}
	previous := debug.SetPanicOnFault(true)
	defer debug.SetPanicOnFault(previous)
	var openedFile *os.File
	defer func() {
		if recover() != nil {
			err = errCacheStructure
			s.disabled = true
		}
		if err != nil {
			s.close()
			// Open may panic before returning a DB handle. Release its file
			// descriptor and flock too, so an explicit rebuild can run next.
			if openedFile != nil {
				openedFile.Close()
			}
		}
	}()
	path := filepath.Join(s.dir, "messages.db")
	if s.rebuild {
		// Under the persistent lock, explicit rebuild also repairs damaged tree
		// pages without asking the database to traverse them first.
		if info, statErr := os.Lstat(path); statErr == nil {
			if !info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1 {
				return errors.New("unsafe search database path")
			}
			if err := os.Remove(path); err != nil {
				return err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}
	opts := &bolt.Options{Timeout: 100 * time.Millisecond, OpenFile: func(path string, flags int, perm os.FileMode) (*os.File, error) {
		f, err := openSearchDatabaseFile(path, flags, perm)
		openedFile = f
		return f, err
	}, NoSync: true, NoGrowSync: true}
	s.db, err = bolt.Open(path, 0600, opts)
	if errors.Is(err, bolterrors.ErrInvalid) || errors.Is(err, bolterrors.ErrChecksum) || errors.Is(err, bolterrors.ErrVersionMismatch) {
		// The lock excludes other CLI readers while replacing a corrupt cache.
		s.warn(errors.New("invalid search database; rebuilding from source"))
		if err = os.Remove(path); err != nil {
			return err
		}
		s.db, err = bolt.Open(path, 0600, opts)
	}
	if err != nil {
		return err
	}
	if !s.rebuild {
		var exists bool
		if err := s.db.View(func(tx *bolt.Tx) error { exists = tx.Bucket(searchBucket) != nil; return nil }); err != nil {
			return err
		}
		if exists {
			return nil
		}
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if s.rebuild && tx.Bucket(searchBucket) != nil {
			if err := tx.DeleteBucket(searchBucket); err != nil {
				return err
			}
		}
		_, err := tx.CreateBucketIfNotExists(searchBucket)
		return err
	})
}

func (s *searchIndex) stage(rel string, record searchIndexRecord) {
	data, err := json.Marshal(record)
	if err != nil {
		s.warn(err)
		s.disabled = true
		return
	}
	if s.pending == nil {
		s.pending = make(map[string][]byte)
	}
	s.pendingBytes -= len(s.pending[rel])
	s.pending[rel] = data
	s.pendingBytes += len(data)
	if len(s.pending) >= 128 || s.pendingBytes >= 4<<20 {
		s.flush()
	}
}

func (s *searchIndex) flush() {
	if s.db == nil || len(s.pending) == 0 {
		return
	}
	if !s.disabled {
		err := cacheAccess(func() error {
			return s.db.Update(func(tx *bolt.Tx) error {
				bucket := tx.Bucket(searchBucket)
				for key, value := range s.pending {
					if err := bucket.Put([]byte(key), value); err != nil {
						return err
					}
				}
				return nil
			})
		})
		if err != nil {
			s.warn(err)
			s.disabled = true
		}
	}
	s.pending = nil
	s.pendingBytes = 0
}

func (s *searchIndex) close() {
	s.flush()
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			s.warn(err)
		}
		s.db = nil
	}
	if s.lock != nil {
		s.lock.Close()
		s.lock = nil
	}
}
