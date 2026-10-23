package root

import (
	"errors"
	"io"
	"os"
	"time"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/config"
	"github.com/hust-open-atom-club/hust-mail-cli/internal/mailarchive"
	"github.com/spf13/cobra"
)

// NewCmdRoot builds an independent command tree, following atomgit-cli's
// constructor pattern, RunE errors and explicit output streams.
func NewCmdRoot(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "hust-mail-cli <command>",
		Short:         "Read HUST mail over IMAP and send over SMTP",
		Long:          "Read and archive HUST email over read-only IMAP, or submit plain-text mail over SMTP. All connections verify TLS; IMAP deletion and flag changes are not implemented.",
		SilenceErrors: true, SilenceUsage: true,
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.PersistentFlags().Bool("json", false, "Output command results as JSON Lines")
	cmd.PersistentFlags().String("config", "", "Config file (default: ~/.config/hust-mail-cli/config.json)")
	cmd.AddCommand(newReceiveCommand("sync", "Download missing messages into a local archive"))
	cmd.AddCommand(newReceiveCommand("folders", "List selectable IMAP folders"))
	var dir string
	list := &cobra.Command{Use: "list", Short: "List local message headers as JSON Lines", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			archiveDir, err := localArchiveDir(cmd, dir)
			if err != nil {
				return err
			}
			return mailarchive.ListContext(cmd.Context(), archiveDir, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	list.Flags().StringVar(&dir, "dir", config.DefaultArchiveDir, "Local archive directory")
	cmd.AddCommand(newSendCommand(), list, newReadCommand(), newSearchCommand(), newAttachmentsCommand())
	return cmd
}

func newReceiveCommand(name, short string) *cobra.Command {
	opts := mailarchive.Options{}
	cmd := &cobra.Command{Use: name, Short: short, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := receiveOptions(cmd, opts)
			if err != nil {
				return err
			}
			return mailarchive.Receive(cmd.Context(), name, resolved, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.User, "user", "", "Full email address (overrides config and HUST_MAIL_USER)")
	flags.DurationVar(&opts.Timeout, "timeout", 30*time.Minute, "Total connection and sync timeout; excludes password input")
	if name == "sync" {
		flags.BoolVar(&opts.RetryIgnored, "retry-ignored", false, "Retry IMAP messages previously recorded as unavailable")
		flags.BoolVar(&opts.Quiet, "quiet", false, "Hide progress output (keep summaries, warnings and errors)")
		flags.IntVar(&opts.Days, "days", 0, "Download only messages received in the last N days; IMAP only, 0 means all time")
		flags.StringVar(&opts.Dir, "dir", config.DefaultArchiveDir, "Local archive directory")
		flags.StringVar(&opts.Mailbox, "mailbox", "", "Sync one IMAP folder; default: all selectable folders")
	}
	return cmd
}

func loadConfig(cmd *cobra.Command) (config.Config, error) {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(path)
}

func receiveOptions(cmd *cobra.Command, flags mailarchive.Options) (mailarchive.Options, error) {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return mailarchive.Options{}, err
	}
	for name, target := range map[string]*string{
		"HUST_MAIL_USER": &cfg.User, "HUST_MAIL_PASSWORD": &cfg.Password,
	} {
		if value, ok := os.LookupEnv(name); ok {
			*target = value
		}
	}
	for name, values := range map[string]struct {
		target *string
		value  string
	}{
		"user":    {&cfg.User, flags.User},
		"dir":     {&cfg.Dir, flags.Dir},
		"mailbox": {&cfg.Mailbox, flags.Mailbox},
	} {
		if cmd.Flags().Changed(name) {
			*values.target = values.value
		}
	}
	if cmd.Flags().Changed("days") {
		cfg.Days = flags.Days
	}
	if cmd.Flags().Changed("timeout") {
		cfg.Timeout = flags.Timeout.String()
	}
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		return mailarchive.Options{}, errors.New("config timeout must be a duration such as 30m")
	}
	archiveDir, err := config.ExpandDir(cfg.Dir)
	if err != nil {
		return mailarchive.Options{}, err
	}
	return mailarchive.Options{User: cfg.User, Password: cfg.Password, Host: config.MailHost,
		Protocol: "imap", Port: config.IMAPPort, Dir: archiveDir, Mailbox: cfg.Mailbox, Timeout: timeout, Days: cfg.Days, JSON: jsonOutput(cmd), Quiet: flags.Quiet, RetryIgnored: flags.RetryIgnored}, nil
}

func jsonOutput(cmd *cobra.Command) bool {
	value, _ := cmd.Root().PersistentFlags().GetBool("json")
	return value
}
