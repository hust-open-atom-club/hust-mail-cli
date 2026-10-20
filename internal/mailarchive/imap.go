package mailarchive

import (
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func listFolders(c *imapclient.Client) ([]string, error) {
	data, err := c.List("", "*", nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	var boxes []string
	for _, box := range data {
		selectable := true
		for _, attr := range box.Attrs {
			if strings.EqualFold(string(attr), `\Noselect`) {
				selectable = false
			}
		}
		if selectable {
			boxes = append(boxes, box.Mailbox)
		}
	}
	return boxes, nil
}

func syncFolder(c *imapclient.Client, root, box string, cutoff time.Time, settings ...imapSyncOptions) (downloaded, skipped int, err error) {
	options := firstIMAPOptions(settings)
	progress := options.progress
	// ReadOnly emits EXAMINE, never SELECT in read/write mode.
	selected, err := c.Select(box, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return 0, 0, err
	}
	if selected.UIDValidity == 0 {
		return 0, 0, errors.New("server omitted UIDVALIDITY; cannot safely identify messages")
	}
	dir := root
	for _, part := range []string{base64.RawURLEncoding.EncodeToString([]byte(box)), strconv.FormatUint(uint64(selected.UIDValidity), 10)} {
		dir = filepath.Join(dir, part)
		if err = privateDir(dir); err != nil {
			return
		}
	}
	criteria := &imap.SearchCriteria{}
	if !cutoff.IsZero() {
		// IMAP SINCE compares server calendar dates, not instants. Widen the
		// candidate day for timezone differences, then check INTERNALDATE exactly.
		criteria.Since = cutoff.UTC().Add(-24 * time.Hour)
	}
	progress.setPhase("searching mail")
	data, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return 0, 0, err
	}
	var failures []error
	var cached int
	uids := data.AllUIDs()
	progress.listed(len(uids))
	for _, uid := range uids {
		progress.message(strconv.FormatUint(uint64(uid), 10))
		path := filepath.Join(dir, strconv.FormatUint(uint64(uid), 10)+".eml")
		info, statErr := os.Lstat(path)
		if statErr == nil {
			if !info.Mode().IsRegular() {
				return downloaded, skipped, fmt.Errorf("archive entry is not a regular file: %s", path)
			}
			if info.Size() > 0 {
				skipped++
				progress.finish("existing")
				continue
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return downloaded, skipped, statErr
		}
		knownIgnored, cacheErr := wasIgnored(dir, uid)
		if cacheErr != nil {
			return downloaded, skipped, cacheErr
		}
		if knownIgnored && !options.retryIgnored {
			cached++
			progress.finish("ignored")
			continue
		}
		if err = atomicWrite(path, func(w io.Writer) error { return fetchMessage(c, uid, progressWriter{w, progress}, cutoff) }); errors.Is(err, errOutsideRange) {
			progress.finish("filtered")
			err = nil
			continue
		} else if err != nil {
			var unavailable *unavailableMessageError
			if errors.As(err, &unavailable) {
				if unavailable.cacheable {
					if cacheErr := rememberIgnored(dir, unavailable); cacheErr != nil {
						return downloaded, skipped, cacheErr
					}
				}
				failures = append(failures, err)
				progress.finish("ignored")
				err = nil
				continue
			}
			return
		}
		if knownIgnored {
			if err = forgetIgnored(dir, uid); err != nil {
				return
			}
		}
		downloaded++
		progress.finish("downloaded")
	}
	if len(failures) > 0 || cached > 0 {
		err = &incompleteFolderError{failures: failures, cached: cached}
	}
	return
}

var errOutsideRange = errors.New("message received before requested cutoff")

// Only missing content may be deferred until the end of a sync. Transport,
// identity and local storage errors still stop immediately.
type unavailableMessageError struct {
	uid       imap.UID
	detail    string
	cacheable bool
	size      int64
}

func (e *unavailableMessageError) Error() string {
	return fmt.Sprintf("UID %d: %s", e.uid, e.detail)
}

type incompleteFolderError struct {
	failures []error
	cached   int
}

func (e *incompleteFolderError) Error() string {
	if len(e.failures) == 0 {
		return fmt.Sprintf("%d cached unavailable messages", e.cached)
	}
	return errors.Join(e.failures...).Error()
}
func (e *incompleteFolderError) Unwrap() []error { return e.failures }

func fetchMessage(c *imapclient.Client, uid imap.UID, w io.Writer, cutoff time.Time) error {
	// Resolve sequence without literals: the library's UID FETCH dispatcher
	// assumes UID precedes the body. Sequence FETCH accepts either order.
	// Verify UID before publishing in case a concurrent expunge shifts numbers.
	resolved, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, InternalDate: !cutoff.IsZero()}).Collect()
	if err != nil {
		return err
	}
	if len(resolved) == 0 {
		return &unavailableMessageError{uid: uid, detail: "server no longer returned the searched UID; retry sync"}
	}
	if len(resolved) != 1 || resolved[0].UID != uid || resolved[0].SeqNum == 0 {
		return fmt.Errorf("cannot resolve UID %d; rerun sync", uid)
	}
	if !cutoff.IsZero() {
		if resolved[0].InternalDate.IsZero() {
			return fmt.Errorf("server omitted INTERNALDATE for UID %d; cannot apply receive-time filter", uid)
		}
		if resolved[0].InternalDate.Before(cutoff) {
			return errOutsideRange
		}
	}
	cmd := c.Fetch(imap.SeqSetNum(resolved[0].SeqNum), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{{Peek: true}}})
	defer cmd.Close()
	count := 0
	var gotUID imap.UID
	var size int64
	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		count++
		for item := msg.Next(); item != nil; item = msg.Next() {
			switch v := item.(type) {
			case imapclient.FetchItemDataUID:
				gotUID = v.UID
			case imapclient.FetchItemDataBodySection:
				n, err := io.Copy(w, v.Literal)
				if err != nil {
					return err
				}
				size += n
			}
		}
	}
	if err := cmd.Close(); err != nil {
		return err
	}
	if count != 1 || gotUID != uid || size == 0 {
		if size == 0 && (count == 0 || (count == 1 && (gotUID == 0 || gotUID == uid))) {
			// A tagged OK without a literal does not establish remote deletion.
			// Query metadata again, without downloading or exposing mail content.
			metadata, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, RFC822Size: true}).Collect()
			if err != nil {
				return err
			}
			if len(metadata) == 0 {
				return &unavailableMessageError{uid: uid, detail: "server no longer returned the UID after the body request; retry sync"}
			}
			if len(metadata) == 1 && metadata[0].UID == uid {
				return &unavailableMessageError{uid: uid, cacheable: true, size: metadata[0].RFC822Size, detail: fmt.Sprintf("server returned no body although the message still exists (RFC822.SIZE=%d bytes); recorded for future syncs; use --retry-ignored to retry", metadata[0].RFC822Size)}
			}
		}
		return fmt.Errorf("incomplete response for UID %d (mail may have been removed remotely); rerun sync", uid)
	}
	return nil
}
