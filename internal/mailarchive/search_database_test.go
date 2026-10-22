package mailarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func assertSearchDatabaseKeys(t *testing.T, dir string, want []string) {
	t.Helper()
	db, err := bolt.Open(filepath.Join(dir, ".search-index", "messages.db"), 0600, &bolt.Options{ReadOnly: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var keys []string
	if err := db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(searchBucket).ForEach(func(k, v []byte) error { keys = append(keys, string(k)); return nil })
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys=%v want=%v", keys, want)
	}
}

func TestSearchMigratesLegacyCacheAndReusesSingleDatabase(t *testing.T) {
	dir := t.TempDir()
	path := searchTestMail(t, dir, "one", "Subject: migration\r\n\r\nneedle")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	idx := searchIndex{root: dir, dir: filepath.Join(dir, ".search-index")}
	if err := privateDir(idx.dir); err != nil {
		t.Fatal(err)
	}
	legacy := searchIndexRecord{Version: searchIndexVersion, Path: "one.eml", Size: info.Size(), ModTime: info.ModTime().UnixNano(), Body: true, Message: parsedMessage{Subject: "migration", Body: "needle"}}
	if err := writeSearchCache(idx.recordPath("one.eml"), legacy); err != nil {
		t.Fatal(err)
	}
	var out, warnings bytes.Buffer
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil || warnings.Len() != 0 || out.Len() == 0 {
		t.Fatalf("migration failed: %v %s", err, &warnings)
	}
	assertSearchDatabaseKeys(t, dir, []string{"one.eml"})
	// A broken legacy file must no longer be opened once the record is in DB.
	if err := os.WriteFile(idx.recordPath("one.eml"), []byte("broken old JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil || warnings.Len() != 0 || out.Len() == 0 {
		t.Fatalf("DB did not supersede legacy cache: %v %s", err, &warnings)
	}
}

func TestSearchDatabaseLockContentionFallsBackAndReleases(t *testing.T) {
	dir := t.TempDir()
	searchTestMail(t, dir, "one", "Subject: lock\r\n\r\nneedle")
	idx := searchIndex{root: dir, dir: filepath.Join(dir, ".search-index"), warn: func(err error) { t.Fatal(err) }}
	idx.prepare(context.Background())
	defer idx.close()
	var out, warnings bytes.Buffer
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil || out.Len() == 0 || warnings.Len() == 0 {
		t.Fatalf("contended cache blocked source search: %v %s", err, &warnings)
	}
	idx.close()
	warnings.Reset()
	out.Reset()
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil || warnings.Len() != 0 {
		t.Fatalf("lock leaked: %v %s", err, &warnings)
	}
}

func TestSearchDatabaseHardLinkIsNotModified(t *testing.T) {
	dir := t.TempDir()
	searchTestMail(t, dir, "one", "Subject: safe\r\n\r\nneedle")
	cache := filepath.Join(dir, ".search-index")
	if err := privateDir(cache); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	want := []byte("keep outside data")
	if err := os.WriteFile(outside, want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(cache, "messages.db")); err != nil {
		t.Fatal(err)
	}
	var warnings bytes.Buffer
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, io.Discard, &warnings); err != nil || warnings.Len() == 0 {
		t.Fatalf("unsafe cache accepted: %v", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("outside data modified", err)
	}
}

func TestSearchRepairsMalformedDatabaseRecord(t *testing.T) {
	dir := t.TempDir()
	searchTestMail(t, dir, "one", "Subject: valid\r\n\r\nneedle")
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	db, err := bolt.Open(filepath.Join(dir, ".search-index", "messages.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(tx *bolt.Tx) error { return tx.Bucket(searchBucket).Put([]byte("one.eml"), []byte("{bad")) })
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	var out, warnings bytes.Buffer
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil || out.Len() == 0 || warnings.Len() == 0 {
		t.Fatalf("corruption fallback failed: %v %s", err, &warnings)
	}
	warnings.Reset()
	out.Reset()
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil || warnings.Len() != 0 {
		t.Fatalf("corrupt record not repaired: %v %s", err, &warnings)
	}
	var row map[string]any
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkSearchCacheLookup(b *testing.B) {
	dir := b.TempDir()
	idx := searchIndex{root: dir, dir: filepath.Join(dir, ".search-index"), warn: func(err error) { b.Fatal(err) }}
	idx.prepare(context.Background())
	defer idx.close()
	var names []string
	var infos []os.FileInfo
	for i := 0; i < 1024; i++ {
		name := fmt.Sprintf("%04d", i)
		path := searchTestMail(b, dir, name, "Subject: cached\r\n\r\nneedle")
		info, err := os.Stat(path)
		if err != nil {
			b.Fatal(err)
		}
		rel := name + ".eml"
		message := parsedMessage{Subject: "cached", Body: strings.Repeat("searchable needle ", 512)}
		idx.save(rel, info, message, true, "")
		record := searchIndexRecord{Version: searchIndexVersion, Path: rel, Size: info.Size(), ModTime: info.ModTime().UnixNano(), Body: true, Message: message}
		if err := writeSearchCache(idx.recordPath(rel), record); err != nil {
			b.Fatal(err)
		}
		names = append(names, rel)
		infos = append(infos, info)
	}
	idx.flush()
	legacy := searchIndex{root: dir, dir: idx.dir, warn: func(err error) { b.Fatal(err) }}
	for _, tc := range []struct {
		name  string
		cache *searchIndex
	}{{"legacy_json", &legacy}, {"single_database", &idx}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				for i, name := range names {
					if _, ok := tc.cache.load(name, infos[i]); !ok {
						b.Fatal("cache miss", name)
					}
				}
			}
		})
	}
}

func TestSearchCorruptTreeFallsBackAndExplicitRebuildRepairs(t *testing.T) {
	dir := t.TempDir()
	searchTestMail(t, dir, "one", "Subject: intact source\r\n\r\nneedle")
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".search-index", "messages.db")
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	pageSize := db.Info().PageSize
	var treePages []int
	err = db.View(func(tx *bolt.Tx) error {
		for i := 2; i < int(tx.Size()/int64(pageSize)); i++ {
			p, err := tx.Page(i)
			if err != nil {
				return err
			}
			if p.Type == "leaf" || p.Type == "branch" {
				treePages = append(treePages, i)
			}
		}
		return nil
	})
	db.Close()
	if err != nil || len(treePages) == 0 {
		t.Fatal("missing tree pages", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range treePages {
		// Bolt pages start with an 8-byte page ID then a 2-byte type flag.
		if _, err := f.WriteAt([]byte{0, 0}, int64(id*pageSize+8)); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	f.Close()
	var out, warnings bytes.Buffer
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil || out.Len() == 0 || warnings.Len() == 0 {
		t.Fatalf("corrupt tree prevented source search: %v %s", err, &warnings)
	}
	out.Reset()
	warnings.Reset()
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle", RebuildIndex: true}, &out, &warnings); err != nil || out.Len() == 0 || warnings.Len() != 0 {
		t.Fatalf("explicit rebuild did not repair tree: %v %s", err, &warnings)
	}
}
