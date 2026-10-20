package mailarchive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type SearchOptions struct {
	Query, Subject, From, To, Body string
	Limit                          int
}

func Search(ctx context.Context, dir string, opts SearchOptions, out io.Writer) error {
	if opts.Limit < 0 {
		return errors.New("--limit must be non-negative")
	}
	if opts.Query+opts.Subject+opts.From+opts.To+opts.Body == "" {
		return errors.New("provide a query or a search field")
	}
	count := 0
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	contains := func(value, query string) bool {
		return strings.Contains(strings.ToLower(value), strings.ToLower(query))
	}
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(path, ".eml") {
			return nil
		}
		m, err := walkMessage(ctx, path, true, nil)
		if err != nil {
			return fmt.Errorf("search %s: %w", path, err)
		}
		if !contains(m.Subject, opts.Subject) || !contains(m.From, opts.From) || !contains(m.To, opts.To) || !contains(m.Body, opts.Body) || !contains(m.Subject+"\n"+m.From+"\n"+m.To+"\n"+m.Body, opts.Query) {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if err := enc.Encode(map[string]any{"path": abs, "subject": m.Subject, "from": m.From, "to": m.To, "date": m.Date, "attachments": len(m.Attachments)}); err != nil {
			return err
		}
		count++
		if opts.Limit > 0 && count >= opts.Limit {
			return filepath.SkipAll
		}
		return nil
	})
}
