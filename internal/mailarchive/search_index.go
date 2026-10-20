package mailarchive

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const searchIndexVersion = 1
const maxIndexRecordBytes = 64 << 20

// This is a disposable cache, not an authority for which mail exists. Every
// search walks the archive and checks the source fingerprint before reuse.
type searchIndexRecord struct {
	Version   int           `json:"version"`
	Path      string        `json:"path"`
	Size      int64         `json:"size"`
	ModTime   int64         `json:"mtime_ns"`
	Body      bool          `json:"has_body"`
	BodyError string        `json:"body_error,omitempty"`
	Message   parsedMessage `json:"message"`
}

type searchIndex struct {
	root, dir string
	disabled  bool
	rebuild   bool
	warn      func(error)
}

func (s *searchIndex) recordPath(rel string) string {
	sum := sha256.Sum256([]byte(rel))
	return filepath.Join(s.dir, fmt.Sprintf("%x.json", sum))
}

func (s *searchIndex) load(rel string, info os.FileInfo) (searchIndexRecord, bool) {
	var record searchIndexRecord
	if s.disabled || s.rebuild {
		return record, false
	}
	path := s.recordPath(rel)
	stat, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return record, false
	}
	if err != nil {
		s.warn(err)
		return record, false
	}
	if !stat.Mode().IsRegular() || stat.Size() > maxIndexRecordBytes {
		s.warn(errors.New("invalid search index entry; rebuilding from source mail"))
		return record, false
	}
	f, err := os.Open(path)
	if err != nil {
		s.warn(err)
		return record, false
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, maxIndexRecordBytes+1))
	if err := dec.Decode(&record); err != nil || dec.Decode(new(any)) != io.EOF {
		s.warn(errors.New("invalid search index JSON; rebuilding from source mail"))
		return searchIndexRecord{}, false
	}
	if record.Version != searchIndexVersion || record.Path != rel || record.Size != info.Size() || record.ModTime != info.ModTime().UnixNano() {
		return searchIndexRecord{}, false
	}
	return record, true
}

func (s *searchIndex) save(rel string, info os.FileInfo, message parsedMessage, body bool, bodyError string) {
	if s.disabled {
		return
	}
	// A changed source must never publish a cache with an outdated fingerprint.
	current, err := os.Stat(filepath.Join(s.root, rel))
	if err != nil || !os.SameFile(info, current) || current.Size() != info.Size() || current.ModTime() != info.ModTime() {
		return
	}
	record := searchIndexRecord{Version: searchIndexVersion, Path: rel, Size: info.Size(), ModTime: info.ModTime().UnixNano(), Body: body, BodyError: bodyError, Message: message}
	if err := atomicWrite(s.recordPath(rel), func(w io.Writer) error { return json.NewEncoder(w).Encode(record) }); err != nil {
		s.warn(err)
		s.disabled = true
	}
}

// Only the RFC 5322 headers are consumed. A broken/large MIME body or attachment
// must not prevent a header match or make a header-only query scan the message.
func searchHeaders(ctx context.Context, path string) (parsedMessage, error) {
	var result parsedMessage
	if err := ctx.Err(); err != nil {
		return result, err
	}
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	r, err := newMIMEReader(ctx, f)
	if err != nil {
		return result, err
	}
	result.Subject, err = r.Header.Subject()
	if err != nil {
		return result, err
	}
	result.From, err = r.Header.Text("From")
	if err != nil {
		return result, err
	}
	result.To, err = r.Header.Text("To")
	result.Date = r.Header.Get("Date")
	return result, err
}

func (s *searchIndex) prepare(ctx context.Context) {
	if s.disabled {
		return
	}
	if err := privateDir(s.dir); err != nil {
		s.warn(err)
		s.disabled = true
		return
	}
	if !s.rebuild {
		return
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		s.warn(err)
		s.disabled = true
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		// Only our hash-named records are disposable; preserve unrelated files.
		name := entry.Name()
		if len(name) != 69 || filepath.Ext(name) != ".json" || entry.IsDir() {
			continue
		}
		valid := true
		for _, r := range name[:64] {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
				valid = false
				break
			}
		}
		if valid {
			if err := os.Remove(filepath.Join(s.dir, name)); err != nil {
				s.warn(err)
				s.disabled = true
				return
			}
		}
	}
}
