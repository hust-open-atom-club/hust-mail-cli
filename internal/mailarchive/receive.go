package mailarchive

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"golang.org/x/term"
)

type Options struct {
	User, Host, Protocol, Dir, Mailbox string
	Password                           string
	Port                               int
	JSON                               bool
	Quiet                              bool
	RetryIgnored                       bool
	Days                               int
	Timeout                            time.Duration
}

func Receive(ctx context.Context, mode string, opts Options, out, errOut io.Writer) error {
	if err := validateOptions(mode, &opts); err != nil {
		return err
	}
	var cutoff time.Time
	if opts.Days > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(opts.Days) * 24 * time.Hour)
	}
	password := opts.Password
	if password == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("set password in config or HUST_MAIL_PASSWORD, or run in a terminal for hidden password input")
		}
		fmt.Fprint(errOut, "Mailbox dedicated password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(errOut)
		if err != nil {
			return err
		}
		password = string(b)
		clear(b)
	}
	if password == "" {
		return errors.New("empty password")
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	var progress *syncProgress
	if mode == "sync" {
		progress = newSyncProgress(errOut, opts.Quiet)
		defer progress.close()
	}
	// TLS is mandatory and certificates are always verified. No SMTP dial path exists.
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 30 * time.Second}, Config: &tls.Config{ServerName: opts.Host, MinVersion: tls.VersionTLS12}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)))
	if err != nil {
		return fmt.Errorf("connect %s/TLS: %w", opts.Protocol, err)
	}
	defer conn.Close()
	// Closing the socket also interrupts a blocked IMAP command or literal read.
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	c := imapclient.New(conn, nil)
	defer c.Close()
	progress.setPhase("authenticating")
	if err := c.Login(opts.User, password).Wait(); err != nil {
		return errors.New("IMAP login failed; check account, password/client authorization code and IMAP access")
	}
	password = ""
	progress.setPhase("listing folders")
	boxes, err := listFolders(c)
	if err != nil {
		return err
	}
	var syncErr error
	if mode == "folders" {
		syncErr = writeFolders(out, boxes, opts.JSON)
	} else {
		progress.pause()
		if !cutoff.IsZero() {
			cutoffOut := out
			if opts.JSON {
				cutoffOut = errOut
			}
			fmt.Fprintf(cutoffOut, "Receive time cutoff: %s (last %d days)\n", cutoff.Format(time.RFC3339), opts.Days)
		}
		if opts.Mailbox != "" {
			boxes = []string{opts.Mailbox}
		}
		root, err := accountDir(opts.Dir, "imap:"+net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)), opts.User)
		if err != nil {
			return err
		}
		unlock, err := lockArchive(root)
		if err != nil {
			return err
		}
		defer unlock()
		meta, _ := json.MarshalIndent(map[string]string{"host": opts.Host, "user": opts.User, "format": "base64url-mailbox/uidvalidity/uid.eml"}, "", "  ")
		if err := atomicWrite(filepath.Join(root, "account.json"), func(w io.Writer) error { _, err := w.Write(meta); return err }); err != nil {
			return err
		}
		syncErr = syncMailboxes(c, root, boxes, cutoff, out, errOut, imapSyncOptions{progress: progress, retryIgnored: opts.RetryIgnored, json: opts.JSON})
	}
	if err := c.Logout().Wait(); err != nil {
		return errors.Join(syncErr, fmt.Errorf("IMAP logout: %w", err))
	}
	return syncErr
}

func syncMailboxes(c *imapclient.Client, root string, boxes []string, cutoff time.Time, out, errOut io.Writer, settings ...imapSyncOptions) error {
	options := firstIMAPOptions(settings)
	progress := options.progress
	reporter := syncOutput{out: out, json: options.json}
	progress.pause()
	if err := reporter.header(boxes); err != nil {
		return err
	}
	var total, existing, ignored int
	for i, box := range boxes {
		progress.beginFolder(box, i+1, len(boxes))
		n, skipped, err := syncFolder(c, root, box, cutoff, options)
		progress.pause()
		count := 0
		if err != nil {
			partial, ok := err.(*incompleteFolderError)
			if !ok {
				return fmt.Errorf("folder %q: %w", box, err)
			}
			count = len(partial.failures) + partial.cached
			if len(partial.failures) > 0 {
				fmt.Fprintf(errOut, "warning: ignoring unavailable mail in folder %q: %v\n", box, err)
			}
		}
		total += n
		existing += skipped
		ignored += count
		if err := reporter.folder(box, n, skipped, count); err != nil {
			return err
		}
	}
	return reporter.complete(root, total, existing, ignored, cutoff)
}

func validateOptions(mode string, opts *Options) error {
	if opts.Days < 0 || opts.Days > 36500 {
		return errors.New("--days/config days must be between 0 and 36500 (0 means all time)")
	}
	if opts.Protocol != "imap" {
		return errors.New("--protocol/config protocol must be imap; POP3 is not supported; use the send command for SMTP sending")
	}
	if opts.Port == 0 {
		opts.Port = 993
	}
	if opts.Port < 1 || opts.Port > 65535 {
		return errors.New("invalid TLS port")
	}
	if opts.User == "" {
		return errors.New("user is required in config, --user or HUST_MAIL_USER")
	}
	if opts.Host == "" || strings.ContainsAny(opts.Host, ":/\\ \t\r\n") {
		return errors.New("--host must be a hostname without port or scheme")
	}
	if opts.Timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	return nil
}
