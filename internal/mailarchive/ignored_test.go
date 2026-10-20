package mailarchive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIgnoredRecordRejectsInvalidOrLinkedState(t *testing.T) {
	for _, kind := range []string{"wrong_uid", "unknown_version", "unknown_reason", "trailing_json", "oversized", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := ignoredPath(dir, 7)
			record := ignoredMessage{Version: 1, UID: 7, Size: 100, Reason: "server_returned_no_body", RecordedAt: time.Now()}
			switch kind {
			case "wrong_uid":
				record.UID = 8
			case "unknown_version":
				record.Version = 2
			case "unknown_reason":
				record.Reason = "other"
			}
			data, _ := json.Marshal(record)
			if kind == "trailing_json" {
				data = append(data, []byte(" {}")...)
			}
			if kind == "oversized" {
				data = append(data, make([]byte, 64*1024)...)
			}
			target := path
			if kind == "symlink" {
				target = filepath.Join(dir, "target")
			}
			if err := os.WriteFile(target, data, 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if ignored, err := wasIgnored(dir, 7); ignored || err == nil {
				t.Fatal("accepted unsafe ignored state")
			}
		})
	}
}
