package root

import (
	"errors"
	"github.com/hust-open-atom-club/hust-mail-cli/internal/config"
	"github.com/hust-open-atom-club/hust-mail-cli/internal/mailarchive"
	"github.com/spf13/cobra"
)

func localArchiveDir(cmd *cobra.Command, dir string) (string, error) {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return "", err
	}
	if cmd.Flags().Changed("dir") {
		cfg.Dir = dir
	}
	return config.ExpandDir(cfg.Dir)
}

func newReadCommand() *cobra.Command {
	var raw bool
	cmd := &cobra.Command{Use: "read <file.eml>", Short: "Display decoded text of a local message", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if raw && jsonOutput(cmd) {
			return errors.New("--raw and --json cannot be used together")
		}
		if jsonOutput(cmd) {
			return mailarchive.ShowJSON(cmd.Context(), args[0], cmd.OutOrStdout())
		}
		if raw {
			return mailarchive.Read(args[0], cmd.OutOrStdout())
		}
		return mailarchive.Show(cmd.Context(), args[0], cmd.OutOrStdout())
	}}
	cmd.Flags().BoolVar(&raw, "raw", false, "Write original MIME bytes instead of decoded text")
	return cmd
}

func newSearchCommand() *cobra.Command {
	var dir string
	var opts mailarchive.SearchOptions
	cmd := &cobra.Command{Use: "search [query]", Short: "Search decoded local headers and body; emit JSON Lines", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			opts.Query = args[0]
		}
		archiveDir, err := localArchiveDir(cmd, dir)
		if err != nil {
			return err
		}
		return mailarchive.Search(cmd.Context(), archiveDir, opts, cmd.OutOrStdout(), cmd.ErrOrStderr())
	}}
	f := cmd.Flags()
	f.StringVar(&dir, "dir", config.DefaultArchiveDir, "Local archive directory")
	f.StringVar(&opts.Subject, "subject", "", "Subject substring (case-insensitive)")
	f.StringVar(&opts.From, "from", "", "Sender substring (case-insensitive)")
	f.StringVar(&opts.To, "to", "", "Recipient substring (case-insensitive)")
	f.StringVar(&opts.Body, "body", "", "Decoded text body substring (case-insensitive)")
	f.StringVar(&opts.After, "after", "", "Date header at or after YYYY-MM-DD (UTC) or RFC3339")
	f.StringVar(&opts.Before, "before", "", "Date header before YYYY-MM-DD (UTC) or RFC3339")
	f.StringVar(&opts.Sort, "sort", "none", "Result order: none, newest, oldest (limit applied after date ordering)")
	f.BoolVar(&opts.Quiet, "quiet", false, "Hide search progress (keep warnings and failure summaries)")
	f.BoolVar(&opts.NoIndex, "no-index", false, "Read source mail without reading or writing the search index")
	f.BoolVar(&opts.RebuildIndex, "rebuild-index", false, "Clear the disposable search index and rebuild it on demand")
	f.IntVar(&opts.Limit, "limit", 0, "Maximum results; 0 means unlimited")
	return cmd
}

func newAttachmentsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "attachments", Short: "List or export decoded local attachments"}
	list := &cobra.Command{Use: "list <file.eml>", Short: "List attachment indices, names, types and decoded sizes", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return mailarchive.ListAttachments(cmd.Context(), args[0], cmd.OutOrStdout())
	}}
	var dir string
	var index int
	export := &cobra.Command{Use: "export <file.eml>", Short: "Export attachments without overwriting existing files", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		dest, err := config.ExpandDir(dir)
		if err != nil {
			return err
		}
		return mailarchive.ExportAttachments(cmd.Context(), args[0], dest, index, cmd.OutOrStdout())
	}}
	export.Flags().StringVar(&dir, "output", "./attachments", "Attachment destination directory")
	export.Flags().IntVar(&index, "index", 0, "Export one attachment by 1-based index; 0 means all")
	cmd.AddCommand(list, export)
	return cmd
}
