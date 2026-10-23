// Package mailsend submits plain-text messages over authenticated SMTP/TLS.
package mailsend

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxBodyBytes = 10 << 20

type Options struct {
	Host, User, Password, Subject, Body string
	Port                                int
	To                                  []string
	Timeout                             time.Duration
}

type Result struct {
	Status     string `json:"status"`
	MessageID  string `json:"message_id"`
	Recipients int    `json:"recipients"`
}

// Validate rejects bad input before opening any network connection.
func Validate(o Options) error {
	if o.Host == "" || strings.ContainsAny(o.Host, "\r\n") || o.Port < 1 || o.Port > 65535 {
		return errors.New("invalid SMTP host or port")
	}
	if o.Timeout <= 0 {
		return errors.New("SMTP timeout must be positive")
	}
	if o.Password == "" {
		return errors.New("SMTP password is required in config or HUST_MAIL_PASSWORD")
	}
	if strings.ContainsAny(o.Subject, "\r\n") {
		return errors.New("subject must not contain newlines")
	}
	if !utf8.ValidString(o.Subject) || !utf8.ValidString(o.Body) {
		return errors.New("subject and body must be valid UTF-8")
	}
	if len(o.Body) > MaxBodyBytes {
		return errors.New("message body exceeds 10 MiB")
	}
	if _, _, err := addresses(o); err != nil {
		return err
	}
	return nil
}

func addresses(o Options) (*mail.Address, []*mail.Address, error) {
	if strings.ContainsAny(o.User, "\r\n") {
		return nil, nil, errors.New("invalid sender address")
	}
	from, err := mail.ParseAddress(o.User)
	if err != nil || from.Address != o.User {
		return nil, nil, errors.New("user must be a plain email address")
	}
	var to []*mail.Address
	for _, raw := range o.To {
		if strings.ContainsAny(raw, "\r\n") {
			return nil, nil, errors.New("invalid recipient address")
		}
		list, err := mail.ParseAddressList(raw)
		if err != nil {
			return nil, nil, errors.New("invalid recipient address")
		}
		to = append(to, list...)
	}
	if len(to) == 0 {
		return nil, nil, errors.New("at least one --to recipient is required")
	}
	// net/smtp does not negotiate internationalized envelope addresses.
	for _, a := range append([]*mail.Address{from}, to...) {
		if !utf8.ValidString(a.Name) {
			return nil, nil, errors.New("display names must be valid UTF-8")
		}
		if !strings.Contains(a.Address, "@") {
			return nil, nil, errors.New("email addresses must include a domain")
		}
		if len((&mail.Address{Address: a.Address}).String()) > 256 {
			return nil, nil, errors.New("SMTP mailbox exceeds the 256-byte path limit")
		}
		for _, r := range a.Address {
			if r > 127 || r < 32 || r == 127 {
				return nil, nil, errors.New("SMTP envelope addresses must be ASCII")
			}
		}
	}
	return from, to, nil
}

// Each RFC 2047 encoded-word fits within 75 bytes. Fold only between words,
// whose separating whitespace decoders ignore, preserving the header text exactly
// even for long ASCII tokens, repeated spaces, tabs and multibyte characters.
func encodedHeaderWords(value string) string {
	var words []string
	for len(value) > 0 {
		n := min(42, len(value))
		for n < len(value) && !utf8.RuneStart(value[n]) {
			n--
		}
		words = append(words, "=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(value[:n]))+"?=")
		value = value[n:]
	}
	return strings.Join(words, "\r\n ")
}

func message(o Options) ([]byte, string, error) {
	from, to, err := addresses(o)
	if err != nil {
		return nil, "", err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, "", errors.New("cannot create message ID")
	}
	id := "<" + hex.EncodeToString(random[:]) + "@" + from.Address[strings.LastIndex(from.Address, "@")+1:] + ">"
	var names []string
	for _, a := range to {
		// Address.String quotes the mailbox correctly but does not fold long
		// display names. Encode the name separately, keeping the angle address
		// on its own continuation line when a name is present.
		address := (&mail.Address{Address: a.Address}).String()
		if a.Name != "" {
			address = encodedHeaderWords(a.Name) + "\r\n " + address
		}
		names = append(names, address)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", from.String(), strings.Join(names, ",\r\n "), encodedHeaderWords(o.Subject), time.Now().Format(time.RFC1123Z), id)
	w := quotedprintable.NewWriter(&b)
	if _, err := w.Write([]byte(o.Body)); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return b.Bytes(), id, nil
}

func Send(ctx context.Context, o Options) (Result, error) {
	return send(ctx, o, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: o.Host})
}

func send(ctx context.Context, o Options, tlsConfig *tls.Config) (Result, error) {
	if err := Validate(o); err != nil {
		return Result{}, err
	}
	data, id, err := message(o)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(o.Host, strconv.Itoa(o.Port)))
	if err != nil {
		return Result{}, errors.New("SMTP connection failed")
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := raw.SetDeadline(deadline); err != nil {
			return Result{}, errors.New("cannot set SMTP deadline")
		}
	}
	conn := tls.Client(raw, tlsConfig)
	if err := conn.HandshakeContext(ctx); err != nil {
		return Result{}, errors.New("SMTP TLS handshake or certificate verification failed")
	}
	c, err := smtp.NewClient(conn, o.Host)
	if err != nil {
		return Result{}, errors.New("SMTP greeting failed")
	}
	defer c.Close()
	if err := c.Auth(smtp.PlainAuth("", o.User, o.Password, o.Host)); err != nil {
		return Result{}, errors.New("SMTP authentication failed")
	}
	from, to, _ := addresses(o)
	if err := c.Mail(from.Address); err != nil {
		return Result{}, errors.New("SMTP sender rejected")
	}
	for _, a := range to {
		if err := c.Rcpt(a.Address); err != nil {
			return Result{}, errors.New("SMTP recipient rejected; message not submitted")
		}
	}
	w, err := c.Data()
	if err != nil {
		return Result{}, errors.New("SMTP DATA rejected; message not submitted")
	}
	if _, err := w.Write(data); err != nil {
		return Result{}, errors.New("SMTP body transmission failed; acceptance unknown, check before retrying")
	}
	if err := w.Close(); err != nil {
		return Result{}, errors.New("SMTP submission failed; acceptance unknown, check before retrying")
	}
	// A successful DATA response confirms acceptance. A later QUIT failure must
	// not turn success into an error that encourages a duplicate submission.
	_ = c.Quit()
	return Result{Status: "submitted", MessageID: id, Recipients: len(to)}, nil
}
