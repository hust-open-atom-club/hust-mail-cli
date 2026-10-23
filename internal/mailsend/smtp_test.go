package mailsend

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSMTPSubmission(t *testing.T) {
	certServer := httptest.NewTLSServer(http.NotFoundHandler())
	cert := certServer.TLS.Certificates[0]
	roots := x509.NewCertPool()
	roots.AddCert(certServer.Certificate())
	certServer.Close()
	for _, mode := range []string{"success", "recipient-rejected", "auth-rejected", "quit-disconnect", "untrusted", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			host, portText, _ := net.SplitHostPort(listener.Addr().String())
			port, _ := strconv.Atoi(portText)
			o := Options{Host: host, Port: port, User: "sender@example.com", Password: "dedicated-secret", To: []string{"接收者 <first@example.com>", "second@example.com"}, Subject: "你好 SMTP", Body: "测试正文\n.leading dot\n", Timeout: 3 * time.Second}
			received := make(chan []byte, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
				if mode == "canceled" {
					var b [1]byte
					_, _ = conn.Read(b[:])
					return
				}
				c := textproto.NewConn(conn)
				_ = c.PrintfLine("220 test SMTP")
				for {
					line, err := c.ReadLine()
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO "):
						_ = c.PrintfLine("250-localhost\r\n250 AUTH PLAIN")
					case strings.HasPrefix(line, "AUTH PLAIN "):
						decoded, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
						if string(decoded) != "\x00"+o.User+"\x00"+o.Password {
							t.Error("unexpected authentication payload")
						}
						if mode == "auth-rejected" {
							_ = c.PrintfLine("535 rejected dedicated-secret")
						} else {
							_ = c.PrintfLine("235 authenticated")
						}
					case line == "*":
						_ = c.PrintfLine("501 authentication canceled")
					case strings.HasPrefix(line, "MAIL FROM:"):
						if !strings.Contains(line, "<sender@example.com>") {
							t.Error("wrong envelope sender")
						}
						_ = c.PrintfLine("250 ok")
					case strings.HasPrefix(line, "RCPT TO:"):
						if mode == "recipient-rejected" && strings.Contains(line, "second@example.com") {
							_ = c.PrintfLine("550 rejected")
						} else {
							_ = c.PrintfLine("250 ok")
						}
					case line == "DATA":
						if mode == "recipient-rejected" {
							t.Error("DATA sent after rejected recipient")
						}
						_ = c.PrintfLine("354 send data")
						data, err := c.ReadDotBytes()
						if err != nil {
							return
						}
						received <- data
						_ = c.PrintfLine("250 queued")
					case line == "QUIT":
						if mode != "quit-disconnect" {
							_ = c.PrintfLine("221 bye")
						}
						return
					default:
						t.Errorf("unexpected SMTP verb: %s", strings.Fields(line)[0])
						return
					}
				}
			}()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				time.AfterFunc(50*time.Millisecond, cancel)
			}
			var result Result
			if mode == "untrusted" {
				result, err = Send(ctx, o)
			} else {
				result, err = send(ctx, o, &tls.Config{RootCAs: roots, ServerName: host, MinVersion: tls.VersionTLS12})
			}
			<-finished
			success := mode == "success" || mode == "quit-disconnect"
			if success != (err == nil) {
				t.Fatalf("unexpected success=%v error=%v", success, err)
			}
			if err != nil && strings.Contains(err.Error(), o.Password) {
				t.Fatal("password leaked in error")
			}
			if !success {
				select {
				case <-received:
					t.Fatal("message transmitted on failed authentication/recipient/TLS/cancellation")
				default:
				}
				return
			}
			if result.Status != "submitted" || result.Recipients != 2 {
				t.Fatalf("bad result: %+v", result)
			}
			raw := <-received
			msg, err := mail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
			if err != nil || subject != o.Subject {
				t.Fatal("subject was not preserved")
			}
			body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
			if err != nil || strings.ReplaceAll(string(body), "\r\n", "\n") != o.Body {
				t.Fatalf("body was not preserved: %q %v", body, err)
			}
			if msg.Header.Get("Message-ID") != result.MessageID {
				t.Fatal("message ID mismatch")
			}
			if _, err := mail.ParseDate(msg.Header.Get("Date")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSendRejectsInvalidInputBeforeConnecting(t *testing.T) {
	good := Options{Host: "localhost", Port: 465, User: "sender@example.com", Password: "secret", To: []string{"to@example.com"}, Subject: "subject", Timeout: time.Second}
	for _, change := range []func(*Options){
		func(o *Options) { o.Subject = "hi\r\nBcc: hidden@example.com" },
		func(o *Options) { o.To = []string{"to@example.com\nBcc: hidden@example.com"} },
		func(o *Options) { o.User = "Name <sender@example.com>" },
		func(o *Options) { o.Password = "" },
		func(o *Options) { o.Body = string([]byte{0xff}) }, func(o *Options) { o.Port = 0 }, func(o *Options) { o.Timeout = 0 },
		func(o *Options) { o.To = nil }, func(o *Options) { o.To = []string{"test"} },
		func(o *Options) { o.Body = strings.Repeat("x", MaxBodyBytes+1) },
	} {
		o := good
		change(&o)
		if _, err := Send(context.Background(), o); err == nil {
			t.Fatal("invalid message accepted")
		}
	}
}

func TestMessageFoldsSubjectWithoutChangingText(t *testing.T) {
	for _, subject := range []string{
		"", "short ASCII subject", strings.Repeat("测", 100),
		strings.Repeat("a", 2000), strings.Repeat("word  with\ttabs ", 100),
		strings.Repeat("中文 and emoji 📨  ", 100),
	} {
		o := Options{User: "sender@example.com", To: []string{"to@example.com"}, Subject: subject, Body: "body"}
		raw, _, err := message(o)
		if err != nil {
			t.Fatal(err)
		}
		header := strings.SplitN(string(raw), "\r\n\r\n", 2)[0]
		for _, line := range strings.Split(header, "\r\n") {
			if len(line) > 78 {
				t.Fatalf("header physical line is %d bytes, want <=78", len(line))
			}
		}
		msg, err := mail.ReadMessage(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
		if err != nil || decoded != subject {
			t.Fatalf("subject changed during folding (input bytes=%d): %v", len(subject), err)
		}
	}
}
