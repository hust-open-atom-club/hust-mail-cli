package mailarchive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	"golang.org/x/net/html"
)

const maxTextBytes = 8 << 20

type Attachment struct {
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type parsedMessage struct {
	Subject, From, To, Date, Body string
	Attachments                   []Attachment
}

// walkMessage decodes MIME transfer encodings, transcoding only display/search
// body text to UTF-8. Attachment bytes retain their original charset and are
// streamed to visit rather than retained in memory. No server is contacted.
func walkMessage(ctx context.Context, path string, body bool, visit func(Attachment, io.Reader) (int64, error)) (parsedMessage, error) {
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
		return result, fmt.Errorf("decode message header: %w", err)
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
	if err != nil {
		return result, err
	}
	result.Date = r.Header.Get("Date")
	var plain, rich []string
	remaining := int64(maxTextBytes)
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		part, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, fmt.Errorf("decode MIME part: %w", err)
		}
		var header mail.AttachmentHeader
		switch h := part.Header.(type) {
		case *mail.InlineHeader:
			header.Header = h.Header
		case *mail.AttachmentHeader:
			header = *h
		}
		kind, params, err := header.ContentType()
		if err != nil {
			return result, err
		}
		name, err := header.Filename()
		if err != nil {
			return result, err
		}
		_, attached := part.Header.(*mail.AttachmentHeader)
		if attached || name != "" || (kind != "text/plain" && kind != "text/html") {
			meta := Attachment{Index: len(result.Attachments) + 1, Filename: name, ContentType: kind}
			if visit != nil {
				meta.Size, err = visit(meta, part.Body)
			} else {
				meta.Size, err = io.Copy(io.Discard, part.Body)
			}
			if err != nil {
				return result, err
			}
			result.Attachments = append(result.Attachments, meta)
			continue
		}
		if !body {
			if _, err := io.Copy(io.Discard, part.Body); err != nil {
				return result, err
			}
			continue
		}
		textBody := part.Body
		if encoding := params["charset"]; encoding != "" {
			decoded, decodeErr := charset.Reader(encoding, textBody)
			if decodeErr != nil {
				// Reader resolves labels without consuming input. Invalid labels
				// often accompany valid UTF-8/ASCII text. UTF-8 decoding retains
				// that text and replaces invalid bytes; attachments stay raw.
				decoded, decodeErr = charset.Reader("utf-8", textBody)
			}
			if decodeErr != nil {
				return result, fmt.Errorf("decode text charset: %w", decodeErr)
			}
			textBody = decoded
		}
		data, err := io.ReadAll(io.LimitReader(textBody, remaining+1))
		if err != nil {
			return result, err
		}
		if int64(len(data)) > remaining {
			return result, errors.New("decoded text exceeds 8 MiB limit; use read --raw")
		}
		remaining -= int64(len(data))
		if kind == "text/plain" {
			plain = append(plain, string(data))
		} else {
			rich = append(rich, htmlText(string(data)))
		}
	}
	if len(plain) != 0 {
		result.Body = strings.Join(plain, "\n")
	} else {
		result.Body = strings.Join(rich, "\n")
	}
	return result, nil
}

func htmlText(source string) string {
	z := html.NewTokenizer(strings.NewReader(source))
	var out strings.Builder
	var hidden string
	for {
		typ := z.Next()
		if typ == html.ErrorToken {
			break
		}
		token := z.Token()
		if hidden != "" {
			if typ == html.EndTagToken && token.Data == hidden {
				hidden = ""
			}
			continue
		}
		if typ == html.StartTagToken && (token.Data == "script" || token.Data == "style" || token.Data == "head" || token.Data == "template") {
			hidden = token.Data
			continue
		}
		if typ == html.TextToken {
			out.WriteString(token.Data)
		}
		if typ == html.StartTagToken || typ == html.EndTagToken || typ == html.SelfClosingTagToken {
			switch token.Data {
			case "p", "div", "br", "li", "tr", "h1", "h2", "h3", "blockquote":
				out.WriteByte('\n')
			case "td", "th":
				out.WriteByte('\t')
			}
		}
	}
	var lines []string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func terminalText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}

func Show(ctx context.Context, path string, out io.Writer) error {
	m, err := walkMessage(ctx, path, true, nil)
	if err != nil {
		return err
	}
	var text strings.Builder
	fmt.Fprintf(&text, "Subject: %s\nFrom: %s\nTo: %s\nDate: %s\n\n%s\n", m.Subject, m.From, m.To, m.Date, m.Body)
	if len(m.Attachments) > 0 {
		text.WriteString("\nAttachments:\n")
		for _, a := range m.Attachments {
			fmt.Fprintf(&text, "  %d. %s (%s, %d bytes)\n", a.Index, a.Filename, a.ContentType, a.Size)
		}
	}
	_, err = io.WriteString(out, terminalText(text.String()))
	return err
}

// ShowJSON emits decoded message text and attachment metadata without modifying it.
func ShowJSON(ctx context.Context, path string, out io.Writer) error {
	m, err := walkMessage(ctx, path, true, nil)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if m.Attachments == nil {
		m.Attachments = []Attachment{}
	}
	return writeJSON(out, map[string]any{
		"path": abs, "subject": m.Subject, "from": m.From, "to": m.To,
		"date": m.Date, "body": m.Body, "attachments": m.Attachments,
	})
}
