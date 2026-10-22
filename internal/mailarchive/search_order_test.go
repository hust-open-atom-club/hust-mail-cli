package mailarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func resultNames(t *testing.T, data []byte) []string {
	t.Helper()
	var result []string
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var row struct{ Path string }
		if err := dec.Decode(&row); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		result = append(result, strings.TrimSuffix(filepath.Base(row.Path), ".eml"))
	}
	return result
}

func TestSearchDateBoundsAndOrderAcrossCacheModes(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, date string }{
		{"a-old", "Tue, 20 Oct 2026 09:00:00 +0000"},
		{"b-tie", "Wed, 21 Oct 2026 16:00:00 +0800"},
		{"c-tie", "Wed, 21 Oct 2026 08:00:00 +0000"},
		{"d-new", "Thu, 22 Oct 2026 09:00:00 +0000"},
		{"e-invalid", "not a date"}, {"f-missing", ""},
	} {
		searchTestMail(t, dir, tc.name, "Subject: dates\r\nDate: "+tc.date+"\r\n\r\nneedle")
	}
	for _, tc := range []struct {
		name    string
		opts    SearchOptions
		want    []string
		skipped bool
	}{
		{"newest_limit", SearchOptions{Body: "needle", Sort: "newest", Limit: 2}, []string{"d-new", "b-tie"}, false},
		{"oldest_limit", SearchOptions{Body: "needle", Sort: "oldest", Limit: 2}, []string{"a-old", "b-tie"}, false},
		{"invalid_last", SearchOptions{Body: "needle", Sort: "newest"}, []string{"d-new", "b-tie", "c-tie", "a-old", "e-invalid", "f-missing"}, false},
		{"day_bounds", SearchOptions{After: "2026-10-21", Before: "2026-10-22", Sort: "oldest"}, []string{"b-tie", "c-tie"}, true},
		{"exact_lower", SearchOptions{After: "2026-10-21T16:00:00+08:00", Before: "2026-10-22T09:00:00Z"}, []string{"b-tie", "c-tie"}, true},
		{"exact_upper", SearchOptions{Before: "2026-10-21T08:00:00Z"}, []string{"a-old"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, noIndex := range []bool{true, false, false} {
				opts := tc.opts
				opts.NoIndex = noIndex
				var out, warnings bytes.Buffer
				if err := Search(context.Background(), dir, opts, &out, &warnings); err != nil {
					t.Fatal(err)
				}
				if got := resultNames(t, out.Bytes()); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("results=%v want=%v", got, tc.want)
				}
				if tc.skipped && !strings.Contains(warnings.String(), "skipped=2") {
					t.Fatalf("missing invalid date diagnostics: %s", &warnings)
				}
				if !tc.skipped && warnings.Len() != 0 {
					t.Fatalf("unexpected diagnostics: %s", &warnings)
				}
			}
		})
	}
}

func TestSearchDateFilterSkipsUnneededBrokenBody(t *testing.T) {
	dir := t.TempDir()
	searchTestMail(t, dir, "old", "Date: Tue, 20 Oct 2026 09:00:00 +0000\r\nContent-Type: multipart/mixed\r\n\r\nbroken MIME")
	var out, warnings bytes.Buffer
	if err := Search(context.Background(), dir, SearchOptions{Body: "needle", After: "2026-10-21"}, &out, &warnings); err != nil || out.Len() != 0 || warnings.Len() != 0 {
		t.Fatalf("date filter parsed excluded body: %v %s", err, &warnings)
	}
}

func TestSearchDateValidationAndSortedOutputFailures(t *testing.T) {
	for _, opts := range []SearchOptions{
		{Query: "x", After: "yesterday"}, {Query: "x", Before: "2026-02-30"},
		{Query: "x", After: "2026-10-22", Before: "2026-10-21"},
		{Query: "x", After: "2026-10-22", Before: "2026-10-22"}, {Query: "x", Sort: "random"},
	} {
		if err := Search(context.Background(), t.TempDir(), opts, io.Discard); err == nil {
			t.Fatalf("invalid options accepted: %+v", opts)
		}
	}
	dir := t.TempDir()
	searchTestMail(t, dir, "one", "Subject: hello\r\n\r\nneedle")
	if err := Search(context.Background(), dir, SearchOptions{Sort: "newest", Limit: 1}, brokenOutput{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("sorted output error lost: %v", err)
	}
}

func TestSearchDateLimitRetainsOnlyBestHeaders(t *testing.T) {
	for _, mode := range []string{"newest", "oldest"} {
		bounded, _ := newSearchOrder(SearchOptions{Sort: mode, Limit: 7})
		all, _ := newSearchOrder(SearchOptions{Sort: mode})
		for i := 0; i < 500; i++ {
			hit := searchHit{date: time.Unix(int64((i*37)%41), 0), path: strings.Repeat("x", i+1), validDate: i%19 != 0}
			bounded.add(hit)
			all.add(hit)
			if bounded.Len() > 7 {
				t.Fatal("limit failed to bound retained headers")
			}
		}
		if !reflect.DeepEqual(bounded.results(), all.results()[:7]) {
			t.Fatal("limited results differ from full date ordering", mode)
		}
	}
}

func TestSortedSearchLimitCountsAllMatches(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, date string }{
		{"a-old", "Tue, 20 Oct 2026 09:00:00 +0000"},
		{"b-middle", "Wed, 21 Oct 2026 09:00:00 +0000"},
		{"c-new", "Thu, 22 Oct 2026 09:00:00 +0000"},
	} {
		searchTestMail(t, dir, tc.name, "Subject: matches\r\nDate: "+tc.date+"\r\n\r\nneedle")
	}
	// A skipped message triggers final statistics without a slow timer test.
	searchTestMail(t, dir, "d-bad", "invalid header\r\n\r\n")
	for _, mode := range []string{"newest", "oldest"} {
		for _, noIndex := range []bool{true, false, false} {
			var out, warnings bytes.Buffer
			err := Search(context.Background(), dir, SearchOptions{Body: "needle", Sort: mode, Limit: 1, NoIndex: noIndex}, &out, &warnings)
			if err != nil {
				t.Fatal(err)
			}
			want := "c-new"
			if mode == "oldest" {
				want = "a-old"
			}
			if got := resultNames(t, out.Bytes()); !reflect.DeepEqual(got, []string{want}) {
				t.Fatalf("limited output=%v want=%s", got, want)
			}
			if !strings.Contains(warnings.String(), "Search: scanned=4 matched=3 skipped=1") {
				t.Fatalf("limited heap suppressed total matches: %s", &warnings)
			}
		}
	}
}
