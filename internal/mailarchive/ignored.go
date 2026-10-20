package mailarchive

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2"
)

// The containing directory already scopes these records by account, mailbox
// and UIDVALIDITY. A record never stands in for a successfully archived .eml.
type ignoredMessage struct {
	Version    int       `json:"version"`
	UID        imap.UID  `json:"uid"`
	Size       int64     `json:"size"`
	Reason     string    `json:"reason"`
	RecordedAt time.Time `json:"recorded_at"`
}

func ignoredPath(dir string, uid imap.UID) string {
	return filepath.Join(dir, ".ignored-"+strconv.FormatUint(uint64(uid), 10)+".json")
}

func wasIgnored(dir string, uid imap.UID) (bool, error) {
	path := ignoredPath(dir, uid)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("ignored-mail record is not a regular file: %s", path)
	}
	if info.Size() > 64*1024 {
		return false, fmt.Errorf("ignored-mail record too large for UID %d", uid)
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 64*1024))
	var record ignoredMessage
	if err := dec.Decode(&record); err != nil {
		return false, fmt.Errorf("invalid ignored-mail record for UID %d", uid)
	}
	if record.Version != 1 || record.UID != uid || record.Reason != "server_returned_no_body" || record.Size < 0 || record.RecordedAt.IsZero() {
		return false, fmt.Errorf("invalid ignored-mail record for UID %d", uid)
	}
	if dec.Decode(new(any)) != io.EOF {
		return false, fmt.Errorf("invalid ignored-mail record for UID %d", uid)
	}
	return true, nil
}

func rememberIgnored(dir string, mail *unavailableMessageError) error {
	record := ignoredMessage{Version: 1, UID: mail.uid, Size: mail.size, Reason: "server_returned_no_body", RecordedAt: time.Now().UTC()}
	return atomicWrite(ignoredPath(dir, mail.uid), func(w io.Writer) error { return json.NewEncoder(w).Encode(record) })
}

func forgetIgnored(dir string, uid imap.UID) error {
	err := os.Remove(ignoredPath(dir, uid))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

type imapSyncOptions struct {
	json         bool
	progress     *syncProgress
	retryIgnored bool
}

func firstIMAPOptions(options []imapSyncOptions) imapSyncOptions {
	if len(options) > 0 {
		return options[0]
	}
	return imapSyncOptions{}
}
