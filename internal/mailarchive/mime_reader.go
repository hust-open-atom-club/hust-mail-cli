package mailarchive

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"sync"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"
)

// mimeReader preserves leaf bytes after transfer decoding. mail.Reader cannot
// be used here: it also transcodes text attachments before exposing their body.
type mimeReader struct {
	Header                 mail.Header
	first                  io.Reader
	stack                  []*textproto.MultipartReader
	ctx                    context.Context
	source                 *io.LimitedReader
	attachmentMetadataOnly bool
}

func newMIMEReader(ctx context.Context, source io.Reader) (*mimeReader, error) {
	limited := &io.LimitedReader{R: &contextReader{ctx: ctx, r: source}, N: 1 << 20}
	buffered := bufio.NewReader(limited)
	header, err := textproto.ReadHeader(buffered)
	if err != nil {
		return nil, err
	}
	limited.N = math.MaxInt64
	return &mimeReader{Header: mail.Header{Header: message.Header{Header: header}}, first: buffered, ctx: ctx, source: limited}, nil
}

// Multipart/base64 readers request small chunks. Buffer the underlying file
// only once body traversal starts; header-only searches keep their small reads.
// Reuse buffers between messages without retaining their file readers.
var mimeBodyBuffers = sync.Pool{New: func() any {
	return bufio.NewReaderSize(nil, 64<<10)
}}

func (r *mimeReader) bufferBodyReads() func() {
	source := r.source.R
	buffered := mimeBodyBuffers.Get().(*bufio.Reader)
	buffered.Reset(source)
	r.source.R = buffered
	return func() {
		r.source.R = source
		buffered.Reset(nil)
		mimeBodyBuffers.Put(buffered)
	}
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
		// Some senders omit the media type but still supply parameters. Keep
		// named/binary parts as attachments instead of treating their bytes as
		// display text; otherwise RFC 2045's text/plain default applies.
		value := strings.TrimSpace(header.Get("Content-Type"))
		if value == "" {
			header.SetContentType("text/plain", nil)
		} else if strings.HasPrefix(value, ";") {
			header.Set("Content-Type", "text/plain"+value)
			_, parameters, parseErr := header.ContentType()
			if parseErr != nil {
				return nil, parseErr
			}
			disposition, dispositionParams, _ := header.ContentDisposition()
			if parameters["name"] != "" || dispositionParams["filename"] != "" || disposition == "attachment" {
				header.Set("Content-Type", "application/octet-stream"+value)
			}
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
		part := &mail.Part{Body: body}
		disposition, _, _ := header.ContentDisposition()
		if disposition == "inline" || (disposition != "attachment" && strings.HasPrefix(kind, "text/")) {
			part.Header = &mail.InlineHeader{Header: header}
		} else {
			part.Header = &mail.AttachmentHeader{Header: header}
		}
		if r.attachmentMetadataOnly {
			name, err := (&mail.AttachmentHeader{Header: header}).Filename()
			if err != nil {
				return nil, err
			}
			_, attached := part.Header.(*mail.AttachmentHeader)
			if attached || name != "" || (kind != "text/plain" && kind != "text/html") {
				return part, nil
			}
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
		part.Body = entity.Body
		return part, nil
	}
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
