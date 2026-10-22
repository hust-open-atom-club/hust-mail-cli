package mailarchive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/emersion/go-message/charset"
)

// Read writes the original MIME message, including encoded attachments.
func Read(path string, out io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(out, f)
	return err
}

// List emits one JSON object per message. It never connects to a server.
func List(dir string, out io.Writer) error {
	return ListContext(context.Background(), dir, out, io.Discard)
}

// ListContext skips individual unreadable messages, but preserves fatal
// directory, cancellation and output errors. It never builds a search cache.
func ListContext(ctx context.Context, dir string, out, warnings io.Writer) error {
	if warnings == nil {
		warnings = io.Discard
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !root.IsDir() {
		return errors.New("list archive must be a real directory")
	}
	var scanned, listed, skipped int
	defer func() {
		if skipped > 0 {
			fmt.Fprintf(warnings, "List: scanned=%d listed=%d skipped=%d\n", scanned, listed, skipped)
		}
	}()
	fail := func(path string, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		skipped++
		fmt.Fprintf(warnings, "warning: skipping unreadable mail %q: %q\n", path, err.Error())
		return nil
	}
	decoder := mime.WordDecoder{CharsetReader: charset.Reader}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".search-index" {
			return filepath.SkipDir
		}
		if entry.IsDir() || !entry.Type().IsRegular() || !strings.HasSuffix(path, ".eml") {
			return nil
		}
		scanned++
		f, err := os.Open(path)
		if err != nil {
			return fail(path, err)
		}
		msg, err := mail.ReadMessage(&listHeaderReader{source: &contextReader{ctx: ctx, r: f}, remaining: 1 << 20})
		f.Close()
		if err != nil {
			return fail(path, err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		subject := msg.Header.Get("Subject")
		if decoded, err := decoder.DecodeHeader(subject); err == nil {
			subject = decoded
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if err := enc.Encode(map[string]string{"path": abs, "subject": subject, "from": msg.Header.Get("From"), "date": msg.Header.Get("Date")}); err != nil {
			return err
		}
		listed++
		return nil
	})
}

var errListHeaderTooLarge = errors.New("message headers exceed 1 MiB limit")

// net/mail accepts EOF after header fields. Unlike io.LimitReader, this reader
// distinguishes actual EOF from exhausting the header budget. The one-byte
// probe also allows a header ending exactly at the limit without a separator.
type listHeaderReader struct {
	source    io.Reader
	remaining int64
}

func (r *listHeaderReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining < 0 {
		return 0, errListHeaderTooLarge
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.source.Read(probe[:])
		if n > 0 {
			r.remaining = -1
			return 0, errListHeaderTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.source.Read(p)
	r.remaining -= int64(n)
	return n, err
}
