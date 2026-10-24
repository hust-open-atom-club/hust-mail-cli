package root

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hust-open-atom-club/hust-mail-cli/internal/commandschema"
	"os"
	"time"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/mailcheck"
	"github.com/spf13/cobra"
)

func newDoctorCommand() *cobra.Command { return newDoctorCommandWithChecker(mailcheck.Check) }

func newDoctorCommandWithChecker(check func(context.Context, mailcheck.Options) ([]mailcheck.Result, error)) *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{Use: "doctor", Short: "Check IMAP/SMTP TLS and authentication without sending mail", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(cmd)
		if err != nil {
			return err
		}
		if v, ok := os.LookupEnv("HUST_MAIL_USER"); ok {
			cfg.User = v
		}
		if v, ok := os.LookupEnv("HUST_MAIL_PASSWORD"); ok {
			cfg.Password = v
		}
		if cmd.Flags().Changed("timeout") {
			cfg.Timeout = timeout.String()
		}
		duration, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return errors.New("config timeout must be a duration such as 30m")
		}
		results, err := check(cmd.Context(), mailcheck.Options{User: cfg.User, Password: cfg.Password, Timeout: duration})
		if err != nil {
			return err
		}
		failed := false
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetEscapeHTML(false)
		for _, r := range results {
			if jsonOutput(cmd) {
				err = enc.Encode(r)
			} else {
				status := "OK"
				if !r.OK {
					status = "FAIL"
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%-4s %-4s %-22s [%s] %s\n", status, r.Protocol, r.Endpoint, r.Stage, r.Message)
			}
			if err != nil {
				return err
			}
			failed = failed || !r.OK
		}
		if failed {
			return errors.New("mail connection checks failed; see doctor results")
		}
		return nil
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Timeout per endpoint (overrides config)")
	commandschema.Annotate(cmd, commandschema.Metadata{
		Positionals: &commandschema.Positionals{MinCount: 0, MaxCount: 0, Description: "No positional arguments"},
		Output:      "text or jsonl with --json",
		Effects:     "none",
		Network:     "imap,smtp",
		Config:      "read",
		Interactive: "never",
		Notes: []string{
			"Authenticates only; no folder selection, message reads, or mail submission.",
			"Timeout must be positive and applies separately to each endpoint. Any failed check returns nonzero.",
			"Uses HUST_MAIL_USER and HUST_MAIL_PASSWORD when set; successful SMTP authentication does not guarantee delivery."},
	})
	return cmd
}
