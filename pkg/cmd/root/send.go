package root

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/mailsend"
	"github.com/spf13/cobra"
)

func newSendCommand() *cobra.Command {
	return newSendCommandWithSender(mailsend.Send)
}

func newSendCommandWithSender(send func(context.Context, mailsend.Options) (mailsend.Result, error)) *cobra.Command {
	var o mailsend.Options
	var file string
	cmd := &cobra.Command{Use: "send", Short: "Submit a plain-text email over authenticated SMTP/TLS", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("body") == cmd.Flags().Changed("body-file") {
			return errors.New("provide exactly one of --body or --body-file")
		}
		cfg, err := loadConfig(cmd)
		if err != nil {
			return err
		}
		resolved := o
		resolved.User, resolved.Password, resolved.Host, resolved.Port = cfg.User, cfg.Password, cfg.SMTP.Host, cfg.SMTP.Port
		if v, ok := os.LookupEnv("HUST_MAIL_USER"); ok {
			resolved.User = v
		}
		if v, ok := os.LookupEnv("HUST_MAIL_PASSWORD"); ok {
			resolved.Password = v
		}
		if cmd.Flags().Changed("user") {
			resolved.User = o.User
		}
		if cmd.Flags().Changed("host") {
			resolved.Host = o.Host
		}
		if cmd.Flags().Changed("port") {
			resolved.Port = o.Port
		}
		if cmd.Flags().Changed("timeout") {
			cfg.Timeout = o.Timeout.String()
		}
		resolved.Timeout, err = time.ParseDuration(cfg.Timeout)
		if err != nil {
			return errors.New("config timeout must be a duration such as 30m")
		}
		if cmd.Flags().Changed("body-file") {
			var r io.Reader = cmd.InOrStdin()
			if file != "-" {
				f, err := os.Open(file)
				if err != nil {
					return errors.New("cannot open body file")
				}
				defer f.Close()
				r = f
			}
			data, err := readSendBody(cmd.Context(), r)
			if err != nil {
				if cmd.Context().Err() != nil {
					return cmd.Context().Err()
				}
				return errors.New("cannot read body file")
			}
			resolved.Body = string(data)
		}
		result, err := send(cmd.Context(), resolved)
		if err != nil {
			return err
		}
		reportSendResult(cmd, result)
		return nil
	}
	f := cmd.Flags()
	f.StringArrayVar(&o.To, "to", nil, "Recipient address or address list; repeat for multiple recipients")
	f.StringVar(&o.Subject, "subject", "", "Message subject")
	f.StringVar(&o.Body, "body", "", "Plain-text UTF-8 body")
	f.StringVar(&file, "body-file", "", "Read UTF-8 body from a file, or - for stdin (maximum 10 MiB)")
	f.StringVar(&o.User, "user", "", "Sender/login address (overrides config and HUST_MAIL_USER)")
	f.StringVar(&o.Host, "host", "", "SMTP implicit-TLS hostname (overrides smtp.host)")
	f.IntVar(&o.Port, "port", 465, "SMTP implicit-TLS port (overrides smtp.port)")
	f.DurationVar(&o.Timeout, "timeout", 30*time.Minute, "Total SMTP connection and submission timeout")
	_ = cmd.MarkFlagRequired("to")
	_ = cmd.MarkFlagRequired("subject")
	return cmd
}

func reportSendResult(cmd *cobra.Command, result mailsend.Result) {
	// Go otherwise terminates the process on EPIPE from stdout/stderr. Once
	// DATA has been accepted, even a closed output pipe must preserve success.
	// Register only around reporting, preserving any other signal listeners.
	pipeSignals := make(chan os.Signal, 1)
	signal.Notify(pipeSignals, syscall.SIGPIPE)
	defer signal.Stop(pipeSignals)
	var err error
	if jsonOutput(cmd) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetEscapeHTML(false)
		err = enc.Encode(result)
	} else {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Submitted to SMTP server: recipients=%d message_id=%s (delivery not confirmed)\n", result.Recipients, result.MessageID)
	}
	if err != nil {
		// Do not let reporting failures encourage a duplicate submission. Avoid
		// echoing arbitrary writer error strings; stderr is best-effort too.
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: SMTP server already accepted message %s; could not write submission result; do not resend automatically\n", result.MessageID)
	}
}

func readSendBody(ctx context.Context, r io.Reader) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		data []byte
		err  error
	}
	// io.Reader has no cancellation operation. Leave ownership of stdin with
	// the caller, but let the command return even if a Read is still pending.
	// The buffered channel lets that read finish without blocking on delivery
	// after cancellation. Owned body files are closed by RunE on return.
	done := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(&sendBodyReader{ctx: ctx, source: r}, mailsend.MaxBodyBytes+1))
		done <- result{data, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case got := <-done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return got.data, got.err
	}
}

type sendBodyReader struct {
	ctx    context.Context
	source io.Reader
}

func (r *sendBodyReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}
