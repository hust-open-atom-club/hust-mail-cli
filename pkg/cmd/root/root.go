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
		Short:         "Read and archive HUST email using IMAP or POP3",
		Long:          "Read and archive HUST email over verified TLS. Remote deletion, flag changes and SMTP sending are not implemented.",
		SilenceErrors: true, SilenceUsage: true,
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.PersistentFlags().String("config", "", "Config file (default: ~/.config/hust-mail-cli/config.json)")
	cmd.AddCommand(newReceiveCommand("sync", "Download missing messages into a local archive"))
	cmd.AddCommand(newReceiveCommand("folders", "List selectable IMAP folders"))
	var dir string
	list := &cobra.Command{Use: "list", Short: "List local message headers as JSON Lines", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("dir") {
				cfg.Dir = dir
			}
			archiveDir, err := config.ExpandDir(cfg.Dir)
			if err != nil {
				return err
			}
			return mailarchive.List(archiveDir, cmd.OutOrStdout())
		},
	}
	list.Flags().StringVar(&dir, "dir", config.DefaultArchiveDir, "Local archive directory")
	read := &cobra.Command{Use: "read <file.eml>", Short: "Write a local message in raw MIME format", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return mailarchive.Read(args[0], cmd.OutOrStdout()) },
	}
	cmd.AddCommand(list, read)
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
	flags.StringVar(&opts.Host, "host", "mail.hust.edu.cn", "Mail server hostname")
	flags.StringVar(&opts.Protocol, "protocol", "imap", "Receiving protocol: imap or pop3")
	flags.IntVar(&opts.Port, "port", 0, "TLS port (default: IMAP 993, POP3 995)")
	flags.DurationVar(&opts.Timeout, "timeout", 30*time.Minute, "Total connection and sync timeout; excludes password input")
	if name == "sync" {
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
		"user": {&cfg.User, flags.User}, "host": {&cfg.Host, flags.Host},
		"protocol": {&cfg.Protocol, flags.Protocol}, "dir": {&cfg.Dir, flags.Dir},
		"mailbox": {&cfg.Mailbox, flags.Mailbox},
	} {
		if cmd.Flags().Changed(name) {
			*values.target = values.value
		}
	}
	if cmd.Flags().Changed("port") {
		cfg.Port = flags.Port
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
	return mailarchive.Options{User: cfg.User, Password: cfg.Password, Host: cfg.Host,
		Protocol: cfg.Protocol, Port: cfg.Port, Dir: archiveDir, Mailbox: cfg.Mailbox, Timeout: timeout}, nil
}
