package mailarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func searchTestMail(t testing.TB, dir, name, raw string) string {
	t.Helper()
	path := filepath.Join(dir, name+".eml")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSearchContinuesAfterBadMail(t *testing.T) {
	dir := t.TempDir()
	messages := []string{
		"not a valid header\r\n\r\n",
		"Subject: broken MIME\r\nContent-Type: multipart/mixed\r\n\r\n",
		"Subject: broken transfer\r\nContent-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\n!!!!",
		"Subject: oversized\r\nContent-Type: text/plain\r\n\r\n" + strings.Repeat("x", maxTextBytes+1),
		"Subject: good\r\n\r\nneedle",
	}
	for i, message := range messages {
		searchTestMail(t, dir, string(rune('a'+i)), message)
	}
	for _, noIndex := range []bool{true, false, false} {
		var out, warnings bytes.Buffer
		if err := Search(context.Background(), dir, SearchOptions{Body: "needle", NoIndex: noIndex}, &out, &warnings); err != nil {
			t.Fatal(err)
		}
		if bytes.Count(out.Bytes(), []byte("\n")) != 1 || !strings.Contains(out.String(), `"subject":"good"`) {
			t.Fatalf("good mail lost after a parse failure: %s", &out)
		}
		if strings.Count(warnings.String(), "skipping unreadable mail") != 4 || !strings.Contains(warnings.String(), "scanned=5 matched=1 skipped=4") {
			t.Fatalf("missing failure diagnostics: %s", &warnings)
		}
	}
}

func TestHeaderOnlySearchDoesNotParseBody(t *testing.T) {
	dir := t.TempDir()
	raw := "Subject: 教职会议\r\nFrom: 测试 <alice@example.com>\r\nTo: bob@example.com\r\n" +
		"Content-Type: multipart/mixed\r\n\r\ninvalid MIME without a boundary"
	path := searchTestMail(t, dir, "broken-body", raw)
	for _, opts := range []SearchOptions{
		{Subject: "教职", From: "ALICE", To: "BOB"},
		{Query: "教职"},
		{Subject: "no-match", Body: "needle"},
	} {
		var out, warnings bytes.Buffer
		if err := Search(context.Background(), dir, opts, &out, &warnings); err != nil {
			t.Fatal(err)
		}
		if warnings.Len() != 0 {
			t.Fatalf("header filter inspected broken body: %s", &warnings)
		}
		if opts.Subject != "no-match" {
			var row map[string]any
			if err := json.Unmarshal(out.Bytes(), &row); err != nil || row["subject"] != "教职会议" {
				t.Fatalf("header search failed: %s %v", &out, err)
			}
			if _, exists := row["attachments"]; exists {
				t.Fatal("unknown attachment count must not be reported as zero")
			}
		}
	}
	var out, warnings bytes.Buffer
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, &out, &warnings); err != nil || out.Len() != 0 || !strings.Contains(warnings.String(), "skipped=1") {
		t.Fatalf("body search did not diagnose broken MIME: %v %s %s", err, &out, &warnings)
	}
	// A cached body failure must not prevent later header queries.
	warnings.Reset()
	if err := Search(context.Background(), dir, SearchOptions{Subject: "教职"}, &out, &warnings); err != nil || warnings.Len() != 0 || out.Len() == 0 {
		t.Fatalf("cached body failure blocked header search: %v %s", err, &warnings)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != raw {
		t.Fatal("source mail modified", err)
	}
}

func TestSearchIndexReuseInvalidationAndRebuild(t *testing.T) {
	dir := t.TempDir()
	path := searchTestMail(t, dir, "message", "Subject: one\r\n\r\nneedle")
	run := func(opts SearchOptions) (string, string) {
		t.Helper()
		var out, warnings bytes.Buffer
		if err := Search(context.Background(), dir, opts, &out, &warnings); err != nil {
			t.Fatal(err)
		}
		return out.String(), warnings.String()
	}
	opts := SearchOptions{Body: "needle"}
	first, warnings := run(opts)
	if first == "" || warnings != "" {
		t.Fatalf("initial search failed: %s %s", first, warnings)
	}
	cacheDir := filepath.Join(dir, ".search-index")
	entries, err := os.ReadDir(cacheDir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("index not created: %v %v", entries, err)
	}
	entry := filepath.Join(cacheDir, "messages.db")
	for _, item := range []struct {
		path string
		perm os.FileMode
	}{{cacheDir, 0700}, {entry, 0600}} {
		info, err := os.Stat(item.path)
		if err != nil || info.Mode().Perm() != item.perm {
			t.Fatalf("unsafe index permissions: %s %v", item.path, err)
		}
	}
	old := time.Unix(1234567890, 0)
	if err := os.Chtimes(entry, old, old); err != nil {
		t.Fatal(err)
	}
	if again, warnings := run(opts); again != first || warnings != "" {
		t.Fatalf("warm search changed results: %s %s", again, warnings)
	}
	info, err := os.Stat(entry)
	if err != nil || !info.ModTime().Equal(old) {
		t.Fatal("unchanged mail was unnecessarily reindexed", err)
	}
	// Same-sized changes must invalidate the cache by source mtime.
	searchTestMail(t, dir, "message", "Subject: two\r\n\r\nneedle")
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if changed, _ := run(opts); !strings.Contains(changed, `"subject":"two"`) {
		t.Fatal("modified source returned stale metadata", changed)
	}
	if err := os.WriteFile(entry, []byte("broken cache"), 0600); err != nil {
		t.Fatal(err)
	}
	if rebuilt, warning := run(opts); rebuilt == "" || !strings.Contains(warning, "index") {
		t.Fatalf("corrupt index did not recover: %s %s", rebuilt, warning)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if deleted, _ := run(opts); deleted != "" {
		t.Fatal("deleted source returned cached result", deleted)
	}
	searchTestMail(t, dir, "new", "Subject: new\r\n\r\nneedle")
	if added, _ := run(opts); !strings.Contains(added, `"subject":"new"`) {
		t.Fatal("new mail absent from indexed search", added)
	}
	// Rebuild removes stale derived entries, never unrelated files.
	unrelated := filepath.Join(cacheDir, "notes.txt")
	if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if rebuilt, _ := run(SearchOptions{Body: "needle", RebuildIndex: true}); !strings.Contains(rebuilt, `"subject":"new"`) {
		t.Fatal("rebuild lost matching source", rebuilt)
	}
	assertSearchDatabaseKeys(t, dir, []string{"new.eml"})
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("rebuild removed unrelated file", err)
	}
}

func TestSearchNoIndexAndUnsafeIndex(t *testing.T) {
	dir := t.TempDir()
	searchTestMail(t, dir, "message", "Subject: test\r\n\r\nneedle")
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle", NoIndex: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(dir, ".search-index")
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatal("--no-index created derived files", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, cacheDir); err != nil {
		t.Fatal(err)
	}
	var out, warnings bytes.Buffer
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle", RebuildIndex: true}, &out, &warnings); err != nil || out.Len() == 0 || warnings.Len() == 0 {
		t.Fatalf("unsafe index did not fall back to source: %v %s %s", err, &out, &warnings)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("followed index symlink", err)
	}
}

func TestSearchFatalFailures(t *testing.T) {
	dir := t.TempDir()
	searchTestMail(t, dir, "message", "Subject: test\r\n\r\nneedle")
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, brokenOutput{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("output failure was skipped", err)
	}
	if err := Search(context.Background(), filepath.Join(dir, "missing"), SearchOptions{Body: "needle"}, io.Discard); err == nil {
		t.Fatal("missing archive accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Search(ctx, dir, SearchOptions{Body: "needle"}, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was skipped", err)
	}
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle", NoIndex: true, RebuildIndex: true}, io.Discard); err == nil {
		t.Fatal("conflicting cache flags accepted")
	}
}

func BenchmarkSearchArchive(b *testing.B) {
	dir := b.TempDir()
	raw := "Subject: staff\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=parts\r\n\r\n" +
		"--parts\r\nContent-Type: text/plain\r\n\r\nneedle\r\n" +
		"--parts\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=large.bin\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		strings.Repeat("QUFB", 65536) + "\r\n--parts--\r\n"
	for i := 0; i < 32; i++ {
		searchTestMail(b, dir, string(rune('A'+i)), raw)
	}
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, io.Discard); err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		opts SearchOptions
	}{
		{"body_without_index", SearchOptions{Body: "needle", NoIndex: true}},
		{"body_rebuild_index", SearchOptions{Body: "needle", RebuildIndex: true}},
		{"body_warm_index", SearchOptions{Body: "needle"}},
		{"headers_without_index", SearchOptions{Subject: "staff", NoIndex: true}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := Search(context.Background(), dir, tc.opts, io.Discard); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestSearchIndexEntrySymlinkDoesNotExposeExternalData(t *testing.T) {
	dir := t.TempDir()
	searchTestMail(t, dir, "message", "Subject: original\r\n\r\nneedle")
	opts := SearchOptions{Body: "needle"}
	if err := Search(context.Background(), dir, opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".search-index"))
	if err != nil || len(entries) != 2 {
		t.Fatal("index missing", err)
	}
	entry := filepath.Join(dir, ".search-index", "messages.db")
	external := filepath.Join(t.TempDir(), "external.json")
	original := []byte("must not be read as a cache or overwritten")
	if err := os.WriteFile(external, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, entry); err != nil {
		t.Fatal(err)
	}
	var out, warnings bytes.Buffer
	if err := Search(context.Background(), dir, opts, &out, &warnings); err != nil || !strings.Contains(out.String(), `"subject":"original"`) || warnings.Len() == 0 {
		t.Fatalf("cache symlink did not recover safely: %v %s %s", err, &out, &warnings)
	}
	got, err := os.ReadFile(external)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("external file modified", err)
	}
	info, err := os.Lstat(entry)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("unsafe cache symlink should remain untouched", err)
	}
}

func TestSearchIndexRejectsSourceChangesAfterSnapshot(t *testing.T) {
	for _, mode := range []string{"replacement", "replacement_same_fingerprint", "modified", "removed"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := searchTestMail(t, dir, "message", "Subject: old\r\n\r\nneedle old")
			stamp := time.Unix(1700000000, 0)
			if err := os.Chtimes(path, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			if err := Search(context.Background(), dir, SearchOptions{Body: "needle"}, io.Discard); err != nil {
				t.Fatal(err)
			}
			// Simulate replacement after WalkDir's d.Info, before cache lookup.
			snapshot, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			updated := "Subject: new\r\n\r\nneedle updated"
			switch mode {
			case "removed":
				err = os.Remove(path)
			case "modified":
				err = os.WriteFile(path, []byte(updated), 0600)
			default:
				if mode == "replacement_same_fingerprint" {
					updated = "Subject: new\r\n\r\nneedle new"
				}
				tmp := filepath.Join(dir, "replacement.tmp")
				if err := os.WriteFile(tmp, []byte(updated), 0600); err != nil {
					t.Fatal(err)
				}
				if mode == "replacement_same_fingerprint" {
					if err := os.Chtimes(tmp, snapshot.ModTime(), snapshot.ModTime()); err != nil {
						t.Fatal(err)
					}
				}
				err = os.Rename(tmp, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "replacement_same_fingerprint" {
				current, err := os.Lstat(path)
				if err != nil || current.Size() != snapshot.Size() || !current.ModTime().Equal(snapshot.ModTime()) || os.SameFile(snapshot, current) {
					t.Fatal("replacement must preserve size/mtime but change identity", err)
				}
			}
			index := searchIndex{root: dir, dir: filepath.Join(dir, ".search-index"), warn: func(err error) { t.Fatal(err) }}
			index.prepare(context.Background())
			defer index.close()
			if record, hit := index.load("message.eml", snapshot); hit {
				t.Fatalf("reused stale cache after %s: %+v", mode, record)
			}
			index.close()
			if mode != "removed" && mode != "replacement_same_fingerprint" {
				var out bytes.Buffer
				if err := Search(context.Background(), dir, SearchOptions{Body: "updated"}, &out); err != nil || !strings.Contains(out.String(), `"subject":"new"`) {
					t.Fatalf("changed source not searchable: %v %s", err, &out)
				}
			}
		})
	}
}
