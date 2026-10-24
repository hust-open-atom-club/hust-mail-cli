// Package mailcheck diagnoses authenticated mail connections without accessing
// messages or submitting mail.
package mailcheck

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/hust-open-atom-club/hust-mail-cli/internal/config"
)

type Options struct {
	User, Password string
	Timeout        time.Duration
}

type Result struct {
	Protocol string `json:"protocol"`
	Endpoint string `json:"endpoint"`
	OK       bool   `json:"ok"`
	Stage    string `json:"stage"`
	Message  string `json:"message"`
}

type dialFunc func(context.Context, string, string) (net.Conn, error)

// Check tests both endpoints independently. Timeout applies to each endpoint.
// Errors from servers are deliberately not returned: they may echo credentials.
func Check(ctx context.Context, o Options) ([]Result, error) {
	return check(ctx, o, (&net.Dialer{}).DialContext, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.MailHost})
}

func check(ctx context.Context, o Options, dial dialFunc, tlsConfig *tls.Config) ([]Result, error) {
	if o.User == "" || o.Password == "" {
		return nil, errors.New("doctor requires user and password in config or HUST_MAIL_USER/HUST_MAIL_PASSWORD")
	}
	if o.Timeout <= 0 {
		return nil, errors.New("doctor timeout must be positive")
	}
	results := make([]Result, 0, 2)
	for _, endpoint := range []struct {
		protocol string
		port     int
	}{{"imap", config.IMAPPort}, {"smtp", config.SMTPPort}} {
		results = append(results, probe(ctx, o, endpoint.protocol, endpoint.port, dial, tlsConfig))
	}
	return results, nil
}

func probe(parent context.Context, o Options, protocol string, port int, dial dialFunc, tlsConfig *tls.Config) Result {
	r := Result{Protocol: protocol, Endpoint: net.JoinHostPort(config.MailHost, strconv.Itoa(port)), Stage: "connect", Message: "DNS lookup or TCP connection failed; check network and firewall"}
	ctx, cancel := context.WithTimeout(parent, o.Timeout)
	defer cancel()
	raw, err := dial(ctx, "tcp", r.Endpoint)
	if err != nil {
		return r
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := raw.SetDeadline(deadline); err != nil {
		return r
	}
	conn := tls.Client(raw, tlsConfig)
	r.Stage, r.Message = "tls", "TLS handshake or certificate verification failed; check system clock and trusted certificates"
	if err := conn.HandshakeContext(ctx); err != nil {
		return r
	}
	r.Stage, r.Message = "greeting", "mail server greeting failed or timed out"
	if protocol == "imap" {
		c := imapclient.New(conn, nil)
		defer c.Close()
		if err := c.WaitGreeting(); err != nil {
			return r
		}
		r.Stage, r.Message = "auth", "authentication failed or timed out; check account, dedicated password and IMAP access"
		if err := c.Login(o.User, o.Password).Wait(); err != nil {
			return r
		}
		// Close the authenticated connection directly. No SELECT, FETCH, writes,
		// or potentially slow logout exchange is needed for this diagnosis.
	} else {
		c, err := smtp.NewClient(conn, config.MailHost)
		if err != nil {
			return r
		}
		defer c.Close()
		r.Stage, r.Message = "auth", "authentication failed or timed out; check account, dedicated password and SMTP access"
		if err := c.Auth(smtp.PlainAuth("", o.User, o.Password, config.MailHost)); err != nil {
			return r
		}
		// No MAIL, RCPT, or DATA commands: successful AUTH is not a delivery test.
	}
	r.OK, r.Stage, r.Message = true, "auth", "TLS and authentication succeeded"
	return r
}
