package mailarchive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SearchOptions struct {
	Query, Subject, From, To, Body string
	Limit                          int
	NoIndex, RebuildIndex          bool
	Quiet                          bool
	After, Before, Sort            string
}

// Search writes result JSON Lines to out and optional diagnostics to errOut.
// Bad individual messages are skipped; directory, cancellation and output
// failures remain fatal. Source mail is never modified.
func Search(ctx context.Context, dir string, opts SearchOptions, out io.Writer, errOut ...io.Writer) error {
	order, err := newSearchOrder(opts)
	if err != nil {
		return err
	}
	if opts.Limit < 0 {
		return errors.New("--limit must be non-negative")
	}
	if opts.NoIndex && opts.RebuildIndex {
		return errors.New("--no-index and --rebuild-index cannot be used together")
	}
	if opts.Query+opts.Subject+opts.From+opts.To+opts.Body+opts.After+opts.Before == "" && !order.sorted() {
		return errors.New("provide a query or a search field")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("search archive must be a real directory")
	}
	warnings := io.Discard
	if len(errOut) > 0 && errOut[0] != nil {
		warnings = errOut[0]
	}
	if warnings != io.Discard {
		warnings = &searchLog{out: warnings}
	}
	var scanned, matched, skipped, cacheHits int
	progress := newSearchProgress(warnings, opts.Quiet)
	defer func() {
		if skipped > 0 || progress.hasOutput() {
			fmt.Fprintf(warnings, "Search: scanned=%d matched=%d skipped=%d cache_hits=%d\n", scanned, matched, skipped, cacheHits)
		}
	}()
	defer progress.close()
	cacheWarned := false
	index := searchIndex{root: root, dir: filepath.Join(root, ".search-index"), disabled: opts.NoIndex, rebuild: opts.RebuildIndex, warn: func(err error) {
		if !cacheWarned {
			fmt.Fprintf(warnings, "warning: search index unavailable or invalid; reading source mail: %q\n", err.Error())
			cacheWarned = true
		}
	}}
	index.prepare(ctx)
	defer index.close()
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	contains := func(value, query string) bool {
		return strings.Contains(strings.ToLower(value), strings.ToLower(query))
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && d.Name() == ".search-index" {
			return filepath.SkipDir
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(path, ".eml") {
			return nil
		}
		scanned++
		progress.update(scanned, matched, skipped, cacheHits)
		fail := func(err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			skipped++
			progress.update(scanned, matched, skipped, cacheHits)
			fmt.Fprintf(warnings, "warning: skipping unreadable mail %q: %q\n", path, err.Error())
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fail(err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		record, cached := index.load(rel, info)
		if cached {
			cacheHits++
			progress.update(scanned, matched, skipped, cacheHits)
		}
		m := record.Message
		var reader *mimeReader
		if !cached {
			// Cache validation may have observed a replacement or removal. Use
			// fresh source metadata for the fallback read and any new cache.
			info, err = os.Lstat(path)
			if err != nil {
				return fail(err)
			}
			if !info.Mode().IsRegular() {
				return fail(errors.New("source mail is no longer a regular file"))
			}
			f, openErr := os.Open(path)
			if openErr != nil {
				return fail(openErr)
			}
			defer f.Close()
			reader, err = newMIMEReader(ctx, f)
			if err != nil {
				return fail(err)
			}
			m, err = readMessageHeaders(reader)
			if err != nil {
				return fail(err)
			}
		}
		if !contains(m.Subject, opts.Subject) || !contains(m.From, opts.From) || !contains(m.To, opts.To) {
			if !cached {
				index.save(rel, info, m, false, "")
			}
			return nil
		}
		dateHeader := m.Date
		var date time.Time
		var dateErr error
		if order.filtered() {
			date, dateErr = mail.ParseDate(m.Date)
		}
		if order.filtered() {
			if !cached {
				index.save(rel, info, m, false, "")
			}
			if dateErr != nil {
				return fail(errors.New("missing or invalid Date header for date filter"))
			}
			if !order.includes(date) {
				return nil
			}
		}
		headerMatch := contains(m.Subject+"\n"+m.From+"\n"+m.To, opts.Query)
		needsBody := opts.Body != "" || opts.Query != "" && !headerMatch
		bodyKnown := cached && record.Body
		if needsBody && !bodyKnown {
			if cached && record.BodyError != "" {
				return fail(errors.New(record.BodyError))
			}
			var parsed parsedMessage
			if reader == nil {
				// A header-only cache hit still needs one source reader for MIME.
				f, openErr := os.Open(path)
				if openErr != nil {
					return fail(openErr)
				}
				defer f.Close()
				reader, err = newMIMEReader(ctx, f)
				if err == nil {
					m, err = readMessageHeaders(reader)
				}
			}
			if err == nil {
				parsed, err = walkMessageParts(ctx, reader, m, messageWalkOptions{body: true, attachmentMetadataOnly: true})
			}
			if err != nil {
				var pathErr *os.PathError
				if ctx.Err() == nil && !errors.As(err, &pathErr) {
					index.save(rel, info, m, false, err.Error())
				}
				return fail(err)
			}
			m, bodyKnown = parsed, true
			index.save(rel, info, m, true, "")
		}
		if !cached && !needsBody {
			index.save(rel, info, m, false, "")
		}
		if !contains(m.Subject, opts.Subject) || !contains(m.From, opts.From) || !contains(m.To, opts.To) || !contains(m.Body, opts.Body) || !contains(m.Subject+"\n"+m.From+"\n"+m.To+"\n"+m.Body, opts.Query) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// An upgraded header-only cache may reopen a changed source. Rank and
		// filter the headers actually decoded with the body, not the old copy.
		if order.sorted() || (order.filtered() && m.Date != dateHeader) {
			date, dateErr = mail.ParseDate(m.Date)
			if order.filtered() {
				if dateErr != nil {
					return fail(errors.New("missing or invalid Date header for date filter"))
				}
				if !order.includes(date) {
					return nil
				}
			}
		}
		result := map[string]any{"path": path, "subject": m.Subject, "from": m.From, "to": m.To, "date": m.Date}
		if bodyKnown {
			result["attachments"] = len(m.Attachments)
		}
		if order.sorted() {
			order.add(searchHit{row: result, date: date, validDate: dateErr == nil, path: path})
			matched++
			progress.update(scanned, matched, skipped, cacheHits)
			return nil
		}
		if err := enc.Encode(result); err != nil {
			return err
		}
		matched++
		progress.update(scanned, matched, skipped, cacheHits)
		if opts.Limit > 0 && matched >= opts.Limit {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, hit := range order.results() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := enc.Encode(hit.row); err != nil {
			return err
		}
	}
	return nil
}
