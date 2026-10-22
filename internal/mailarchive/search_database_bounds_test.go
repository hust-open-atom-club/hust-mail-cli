package mailarchive

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestSearchRecoversTruncatedCacheInProcess(t *testing.T) {
	for _, size := range []int64{32768, 40960, 49152} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			dir := t.TempDir()
			raw := "Subject: preserved\r\n\r\n" + strings.Repeat("needle ", 16384)
			mail := searchTestMail(t, dir, "one", raw)
			if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, io.Discard); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, ".search-index", "messages.db")
			info, err := os.Stat(path)
			if err != nil || info.Size() <= size {
				t.Fatal("fixture too small", err)
			}
			if err := os.Truncate(path, size); err != nil {
				t.Fatal(err)
			}
			// A process boundary ensures a regression to a fatal mmap fault is reported.
			cmd := exec.Command(os.Args[0], "-test.run=^TestSearchTruncatedCacheHelper$")
			cmd.Env = append(os.Environ(), "HUST_MAIL_TRUNCATED_CACHE_HELPER="+dir)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("truncated cache crashed search: %v\n%s", err, output)
			}
			got, err := os.ReadFile(mail)
			if err != nil || string(got) != raw {
				t.Fatal("source message changed", err)
			}
		})
	}
}

func TestSearchTruncatedCacheHelper(t *testing.T) {
	dir := os.Getenv("HUST_MAIL_TRUNCATED_CACHE_HELPER")
	if dir == "" {
		return
	}
	var out, warnings bytes.Buffer
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"subject":"preserved"`) || !strings.Contains(warnings.String(), "index") {
		t.Fatalf("missing result or recovery warning: %s %s", &out, &warnings)
	}
	warnings.Reset()
	out.Reset()
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil || out.Len() == 0 || warnings.Len() != 0 {
		t.Fatalf("repaired cache cannot be reused: %v %s", err, &warnings)
	}
}

func TestCacheBoundsAllowDifferentPageSizesAndSingleBadMeta(t *testing.T) {
	for _, pageSize := range []int{4096, 8192, 65536} {
		for _, badFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/bad-first=%v", pageSize, badFirst), func(t *testing.T) {
				dir := t.TempDir()
				path := searchTestMail(t, dir, "one", "Subject: source\r\n\r\nneedle")
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				cache := filepath.Join(dir, ".search-index")
				if err := privateDir(cache); err != nil {
					t.Fatal(err)
				}
				dbPath := filepath.Join(cache, "messages.db")
				db, err := bolt.Open(dbPath, 0600, &bolt.Options{PageSize: pageSize})
				if err != nil {
					t.Fatal(err)
				}
				record := searchIndexRecord{Version: searchIndexVersion, Path: "one.eml", Size: info.Size(), ModTime: info.ModTime().UnixNano(), Body: true, Message: parsedMessage{Subject: "source", Body: "needle"}}
				data, err := json.Marshal(record)
				if err != nil {
					db.Close()
					t.Fatal(err)
				}
				err = db.Update(func(tx *bolt.Tx) error {
					bucket, err := tx.CreateBucketIfNotExists(searchBucket)
					if err != nil {
						return err
					}
					return bucket.Put([]byte("one.eml"), data)
				})
				var txID int
				if err == nil {
					err = db.View(func(tx *bolt.Tx) error { txID = tx.ID(); return nil })
				}
				// Keep the latest generation in page 1 before damaging page 0.
				if err == nil && txID%2 == 0 {
					err = db.Update(func(*bolt.Tx) error { return nil })
				}
				db.Close()
				if err != nil {
					t.Fatal(err)
				}
				if badFirst {
					f, err := os.OpenFile(dbPath, os.O_RDWR, 0600)
					if err != nil {
						t.Fatal(err)
					}
					_, err = f.WriteAt([]byte{0}, 16) // Damage only meta page 0's magic.
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
				var warnings bytes.Buffer
				index := searchIndex{root: dir, dir: cache, warn: func(err error) { fmt.Fprintln(&warnings, err) }}
				index.prepare(context.Background())
				defer index.close()
				got, hit := index.load("one.eml", info)
				if !hit || !got.Body || got.Message.Body != "needle" || warnings.Len() != 0 {
					t.Fatalf("valid cache rejected: hit=%v record=%+v warnings=%s", hit, got, &warnings)
				}
			})
		}
	}
}

func TestSearchRecoversInvalidLeafOffset(t *testing.T) {
	for _, location := range []string{"root", "inline", "leaf"} {
		t.Run(location, func(t *testing.T) {
			dir := t.TempDir()
			raw := "Subject: preserved\r\n\r\nneedle"
			if location == "leaf" {
				raw += strings.Repeat("large body ", 16384)
			}
			mail := searchTestMail(t, dir, "one", raw)
			if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, io.Discard); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, ".search-index", "messages.db")
			f, err := os.OpenFile(path, os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			first, ok := readCacheMeta(f, 0, 0)
			if !ok {
				t.Fatal("invalid fixture metadata")
			}
			meta := first
			if second, ok := readCacheMeta(f, int64(first.pageSize), 1); ok && second.txID > meta.txID {
				meta = second
			}
			element := int64(meta.root*meta.pageSize) + 16
			if location != "root" {
				var leaf [16]byte
				if _, err := f.ReadAt(leaf[:], element); err != nil {
					t.Fatal(err)
				}
				value := element + int64(binary.NativeEndian.Uint32(leaf[4:8])) + int64(binary.NativeEndian.Uint32(leaf[8:12]))
				var bucket [16]byte
				if _, err := f.ReadAt(bucket[:], value); err != nil {
					t.Fatal(err)
				}
				root := binary.NativeEndian.Uint64(bucket[:8])
				if location == "inline" {
					if root != 0 {
						t.Fatal("fixture bucket is not inline")
					}
					element = value + 16 + 16
				} else {
					if root == 0 {
						t.Fatal("fixture bucket is inline")
					}
					element = int64(root*meta.pageSize) + 16
				}
			}
			var offset [4]byte
			binary.NativeEndian.PutUint32(offset[:], 0x10000000)
			if _, err := f.WriteAt(offset[:], element+4); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestSearchInvalidLeafHelper$")
			cmd.Env = append(os.Environ(), "HUST_MAIL_INVALID_LEAF_HELPER="+dir)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("invalid leaf crashed search: %v\n%s", err, output)
			}
			if got, err := os.ReadFile(mail); err != nil || string(got) != raw {
				t.Fatal("source message changed", err)
			}
		})
	}
}

func TestSearchInvalidLeafHelper(t *testing.T) {
	dir := os.Getenv("HUST_MAIL_INVALID_LEAF_HELPER")
	if dir == "" {
		return
	}
	previous := debug.SetPanicOnFault(false)
	defer debug.SetPanicOnFault(previous)
	var out, warnings bytes.Buffer
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil {
		t.Fatal(err)
	}
	if old := debug.SetPanicOnFault(false); old {
		t.Fatal("search leaked the mmap fault policy")
	}
	if !strings.Contains(out.String(), `"subject":"preserved"`) || !strings.Contains(warnings.String(), "index") {
		t.Fatalf("missing fallback result or warning: %s %s", &out, &warnings)
	}
	out.Reset()
	warnings.Reset()
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle", RebuildIndex: true}, &out, &warnings); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	warnings.Reset()
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil || out.Len() == 0 || warnings.Len() != 0 {
		t.Fatalf("rebuilt cache cannot be reused: %v %s", err, &warnings)
	}
}

func TestCacheAccessRestoresFaultPolicy(t *testing.T) {
	for _, initial := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("initial=%v/panic=%v", initial, fail), func(t *testing.T) {
				previous := debug.SetPanicOnFault(initial)
				defer debug.SetPanicOnFault(previous)
				err := cacheAccess(func() error {
					if old := debug.SetPanicOnFault(true); !old {
						t.Fatal("cache access is not guarded")
					}
					if fail {
						panic("damaged cache")
					}
					return nil
				})
				if errors.Is(err, errCacheStructure) != fail {
					t.Fatalf("unexpected cache result: %v", err)
				}
				if got := debug.SetPanicOnFault(initial); got != initial {
					t.Fatalf("fault policy leaked: got %v, want %v", got, initial)
				}
			})
		}
	}
}

func TestSearchRecoversCyclicBranchReferences(t *testing.T) {
	for _, cycle := range []string{"self", "ancestor"} {
		t.Run(cycle, func(t *testing.T) {
			dir := t.TempDir()
			raw := "Subject: preserved\r\n\r\nneedle"
			source := searchTestMail(t, dir, "one", raw)
			if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, io.Discard); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, ".search-index", "messages.db")
			db, err := bolt.Open(path, 0600, nil)
			if err != nil {
				t.Fatal(err)
			}
			pageSize := uint64(db.Info().PageSize)
			err = db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket(searchBucket)
				for i := 0; i < 200; i++ {
					if err := b.Put([]byte(fmt.Sprintf("fixture-%04d", i)), bytes.Repeat([]byte("x"), 256)); err != nil {
						return err
					}
				}
				return nil
			})
			var root uint64
			if err == nil {
				err = db.View(func(tx *bolt.Tx) error { root = uint64(tx.Bucket(searchBucket).Root()); return nil })
			}
			db.Close()
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.OpenFile(path, os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			var page [33]byte
			if _, err := f.ReadAt(page[:], int64(root*pageSize)); err != nil {
				t.Fatal(err)
			}
			if binary.NativeEndian.Uint16(page[8:10]) != 1 {
				t.Fatal("fixture root is not a branch")
			}
			target := root*pageSize + 24
			if cycle == "ancestor" {
				child := binary.NativeEndian.Uint64(page[24:32])
				// Turn the child into a valid one-element branch pointing to its parent.
				var branch [33]byte
				binary.NativeEndian.PutUint64(branch[:8], child)
				binary.NativeEndian.PutUint16(branch[8:10], 1)
				binary.NativeEndian.PutUint16(branch[10:12], 1)
				binary.NativeEndian.PutUint32(branch[16:20], 16)
				binary.NativeEndian.PutUint32(branch[20:24], 1)
				binary.NativeEndian.PutUint64(branch[24:32], root)
				if _, err := f.WriteAt(branch[:], int64(child*pageSize)); err != nil {
					t.Fatal(err)
				}
			} else {
				var id [8]byte
				binary.NativeEndian.PutUint64(id[:], root)
				if _, err := f.WriteAt(id[:], int64(target)); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSearchInvalidLeafHelper$")
			cmd.Env = append(os.Environ(), "HUST_MAIL_INVALID_LEAF_HELPER="+dir)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("cyclic cache prevented search: %v\n%s", err, output)
			}
			if got, err := os.ReadFile(source); err != nil || string(got) != raw {
				t.Fatal("source message changed", err)
			}
		})
	}
}
