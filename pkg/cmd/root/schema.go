package root

import (
	"encoding/json"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/commandschema"
	"github.com/spf13/cobra"
)

func newSchemaCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "schema [<command> ...]", Short: "Describe public commands as versioned JSON without executing them", Long: "List public commands or describe an exact command path using static Cobra metadata. No configuration, credentials, mailbox, or network is accessed. This versioned command description is not JSON Schema; unspecified behavior must not be inferred.", Example: "  hust-mail-cli schema\n  hust-mail-cli schema send\n  hust-mail-cli schema attachments export\n  hust-mail-cli schema hust-mail-cli", Args: cobra.ArbitraryArgs}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		doc, err := commandschema.Describe(cmd.Root(), args)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetEscapeHTML(false)
		return enc.Encode(doc)
	}
	commandschema.Annotate(cmd, commandschema.Metadata{Positionals: &commandschema.Positionals{MinCount: 0, MaxCount: -1, Description: "Exact command path components, optionally prefixed with hust-mail-cli; omit to list commands, or use hust-mail-cli alone for root details. No prefix matching."}, Output: "json", Effects: "none", Network: "none", Config: "none", Interactive: "never"})
	return cmd
}

// Cobra-generated utility commands have no mailbox or configuration effects.
// Their positional contracts are left undescribed rather than inferred.
func annotateUtility(cmd *cobra.Command) {
	commandschema.Annotate(cmd, commandschema.Metadata{Output: "text", Effects: "none", Network: "none", Config: "none", Interactive: "never"})
	for _, child := range cmd.Commands() {
		annotateUtility(child)
	}
}
