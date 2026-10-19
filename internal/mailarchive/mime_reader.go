package mailarchive

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math"
	"strings"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"
)

// mimeReader preserves leaf bytes after transfer decoding. mail.Reader cannot
// be used here: it also transcodes text attachments before exposing their body.
type mimeReader struct {
	Header mail.Header
	first  io.Reader
	stack  []*textproto.MultipartReader
	ctx    context.Context
}

func newMIMEReader(ctx context.Context, source io.Reader) (*mimeReader, error) {
	limited := &io.LimitedReader{R: source, N: 1 << 20}
	buffered := bufio.NewReader(limited)
	header, err := textproto.ReadHeader(buffered)
	if err != nil {
		return nil, err
	}
	limited.N = math.MaxInt64
	return &mimeReader{Header: mail.Header{Header: message.Header{Header: header}}, first: buffered, ctx: ctx}, nil
}

func (r *mimeReader) NextPart() (*mail.Part, error) {
	for {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
		var header message.Header
		var body io.Reader
		if r.first != nil {
			header, body = r.Header.Header, r.first
			r.first = nil
		} else if len(r.stack) > 0 {
			part, err := r.stack[len(r.stack)-1].NextPart()
			if err == io.EOF {
				r.stack = r.stack[:len(r.stack)-1]
				continue
			}
			if err != nil {
				return nil, err
			}
			header, body = message.Header{Header: part.Header}, part
		} else {
			return nil, io.EOF
		}
		kind, params, err := header.ContentType()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(kind, "multipart/") {
			if len(r.stack) >= 64 {
				return nil, errors.New("MIME nesting exceeds 64 levels")
			}
			if params["boundary"] == "" {
				return nil, errors.New("multipart message has no boundary")
			}
			// As in go-message, ignore non-conformant multipart transfer encodings.
			r.stack = append(r.stack, textproto.NewMultipartReader(body, params["boundary"]))
			continue
		}
		// Retain go-message's transfer decoders, including MIME Base64 whitespace
		// handling, but suppress its implicit text/* charset conversion. Original
		// headers remain available for filenames and explicit body conversion.
		transferHeader := header.Copy()
		transferHeader.SetContentType("application/octet-stream", nil)
		entity, err := message.New(transferHeader, body)
		if err != nil {
			return nil, err
		}
		part := &mail.Part{Body: entity.Body}
		disposition, _, _ := header.ContentDisposition()
		if disposition == "inline" || (disposition != "attachment" && strings.HasPrefix(kind, "text/")) {
			part.Header = &mail.InlineHeader{Header: header}
		} else {
			part.Header = &mail.AttachmentHeader{Header: header}
		}
		return part, nil
	}
}
