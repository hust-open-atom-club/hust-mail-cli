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

func syncFolder(c *imapclient.Client, root, box string, cutoff time.Time) (downloaded, skipped int, err error) {
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
	data, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return 0, 0, err
	}
	for _, uid := range data.AllUIDs() {
		path := filepath.Join(dir, strconv.FormatUint(uint64(uid), 10)+".eml")
		info, statErr := os.Lstat(path)
		if statErr == nil {
			if !info.Mode().IsRegular() {
				return downloaded, skipped, fmt.Errorf("archive entry is not a regular file: %s", path)
			}
			if info.Size() > 0 {
				skipped++
				continue
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return downloaded, skipped, statErr
		}
		if err = atomicWrite(path, func(w io.Writer) error { return fetchMessage(c, uid, w, cutoff) }); errors.Is(err, errOutsideRange) {
			err = nil
			continue
		} else if err != nil {
			return
		}
		downloaded++
	}
	return
}

var errOutsideRange = errors.New("message received before requested cutoff")

func fetchMessage(c *imapclient.Client, uid imap.UID, w io.Writer, cutoff time.Time) error {
	// Resolve sequence without literals: the library's UID FETCH dispatcher
	// assumes UID precedes the body. Sequence FETCH accepts either order.
	// Verify UID before publishing in case a concurrent expunge shifts numbers.
	resolved, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, InternalDate: !cutoff.IsZero()}).Collect()
	if err != nil {
		return err
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
		return fmt.Errorf("incomplete response for UID %d (mail may have been removed remotely); rerun sync", uid)
	}
	return nil
}
