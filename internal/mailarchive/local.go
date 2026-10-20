package mailarchive

import (
	"encoding/json"
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
	decoder := mime.WordDecoder{CharsetReader: charset.Reader}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() || !strings.HasSuffix(path, ".eml") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		msg, err := mail.ReadMessage(f)
		f.Close()
		if err != nil {
			return err
		}
		subject := msg.Header.Get("Subject")
		if decoded, err := decoder.DecodeHeader(subject); err == nil {
			subject = decoded
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		return enc.Encode(map[string]string{"path": abs, "subject": subject, "from": msg.Header.Get("From"), "date": msg.Header.Get("Date")})
	})
}
