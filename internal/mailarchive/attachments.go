package mailarchive

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ListAttachments(ctx context.Context, path string, out io.Writer) error {
	m, err := walkMessage(ctx, path, false, nil)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for _, a := range m.Attachments {
		if err := enc.Encode(a); err != nil {
			return err
		}
	}
	return nil
}

func attachmentName(a Attachment) string {
	name := filepath.Base(strings.ReplaceAll(a.Filename, "\\", "/"))
	name = strings.TrimSpace(terminalText(name))
	name = strings.ReplaceAll(strings.ReplaceAll(name, "\n", "_"), "\t", "_")
	if name == "" || name == "." || name == ".." {
		name = "attachment"
	}
	return fmt.Sprintf("%03d-%s", a.Index, name)
}

// ExportAttachments writes decoded attachment bytes, never overwriting an
// existing entry. OpenRoot confines all operations to the output directory.
func ExportAttachments(ctx context.Context, path, dir string, index int, out io.Writer) error {
	if index < 0 {
		return errors.New("--index must be positive (or omitted for all attachments)")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	type staged struct {
		temp, name string
		size       int64
	}
	var files []staged
	defer func() {
		for _, f := range files {
			_ = root.Remove(f.temp)
		}
	}()
	_, err = walkMessage(ctx, path, false, func(a Attachment, r io.Reader) (int64, error) {
		if index != 0 && a.Index != index {
			return io.Copy(io.Discard, r)
		}
		name := attachmentName(a)
		tmp := ".attachment-" + rand.Text()
		f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return 0, err
		}
		files = append(files, staged{temp: tmp, name: name})
		n, copyErr := io.Copy(f, r)
		if copyErr == nil {
			copyErr = f.Sync()
		}
		closeErr := f.Close()
		if copyErr != nil {
			return n, copyErr
		}
		if closeErr != nil {
			return n, closeErr
		}
		files[len(files)-1].size = n
		return n, nil
	})
	if err != nil {
		return err
	}
	if index > 0 && len(files) == 0 {
		return fmt.Errorf("attachment index %d not found", index)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var published []string
	for _, f := range files {
		// Hard-link publication is atomic and fails if a file or symlink exists.
		if err := root.Link(f.temp, f.name); err != nil {
			for _, name := range published {
				_ = root.Remove(name)
			}
			return fmt.Errorf("export attachment (existing files are preserved): %w", err)
		}
		published = append(published, f.name)
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for _, f := range files {
		abs, err := filepath.Abs(filepath.Join(dir, f.name))
		if err != nil {
			return err
		}
		if err := enc.Encode(map[string]any{"path": abs, "size": f.size}); err != nil {
			return err
		}
	}
	return nil
}
