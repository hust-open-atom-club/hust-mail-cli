package commandschema

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestDescribeReadsOnlyStaticRegistrations(t *testing.T) {
	root := &cobra.Command{Use: "mail <command>", PersistentPreRun: func(*cobra.Command, []string) { t.Fatal("hook executed") }}
	root.PersistentFlags().String("config", "", "config path")
	child := &cobra.Command{Use: "send", Aliases: []string{"submit"}, Args: func(*cobra.Command, []string) error { t.Fatal("validator executed"); return nil }, Run: func(*cobra.Command, []string) { t.Fatal("handler executed") }}
	child.Flags().String("to", "static-default", "recipient")
	child.Flags().String("body", "", "body")
	child.Flags().String("body-file", "", "body file")
	child.Flags().String("secret-flag", "hidden", "hidden")
	_ = child.Flags().MarkHidden("secret-flag")
	_ = child.MarkFlagRequired("to")
	child.MarkFlagsOneRequired("body", "body-file")
	child.MarkFlagsMutuallyExclusive("body", "body-file")
	root.AddCommand(child, &cobra.Command{Use: "hidden", Hidden: true})
	Annotate(child, Metadata{Positionals: &Positionals{MinCount: 0, MaxCount: 0, Description: "none"}, Output: "json", Effects: "smtpSend"})
	_ = child.Flags().Set("to", "runtime-secret")
	_ = root.PersistentFlags().Set("config", "private-runtime-path")
	doc, err := Describe(root, []string{"mail", "submit"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.FormatVersion != 1 || doc.Command.Path != "mail send" || doc.Command.Effects != "smtpSend" {
		t.Fatal("bad command description")
	}
	found := map[string]Flag{}
	for _, f := range doc.Command.Flags {
		found[f.Name] = f
	}
	if found["to"].Default != "static-default" || !found["to"].Required || !found["config"].Inherited || found["config"].DefinedOn != "mail" {
		t.Fatal("flag metadata incorrect")
	}
	if _, ok := found["secret-flag"]; ok {
		t.Fatal("hidden flag exposed")
	}
	wantGroups := []FlagGroup{{Kind: "atLeastOneRequired", Flags: []string{"body", "body-file"}}, {Kind: "mutuallyExclusive", Flags: []string{"body", "body-file"}}}
	if !reflect.DeepEqual(doc.Command.FlagGroups, wantGroups) {
		t.Fatalf("groups: %v", doc.Command.FlagGroups)
	}
	data, _ := json.Marshal(doc)
	if strings.Contains(string(data), "runtime-secret") || strings.Contains(string(data), "private-runtime-path") {
		t.Fatal("runtime values exposed")
	}
	for _, path := range [][]string{{"sen"}, {"hidden"}, {"send", "unknown"}} {
		if _, err := Describe(root, path); err == nil {
			t.Fatalf("accepted %v", path)
		}
	}
	listing, err := Describe(root, nil)
	if err != nil || len(listing.Commands) != 2 {
		t.Fatal("incorrect visible command list", err)
	}
	again, _ := Describe(root, []string{"send"})
	a, _ := json.Marshal(again)
	if string(data) != string(a) {
		t.Fatal("schema is not deterministic")
	}
}

func TestIncompleteMetadataIsExplicit(t *testing.T) {
	root := &cobra.Command{Use: "mail"}
	doc, err := Describe(root, []string{"mail"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Command.Validation != "partial" || !strings.Contains(strings.Join(doc.Command.Undescribed, ","), "positionals") {
		t.Fatal("inferred missing semantics")
	}
	if doc.Command.Network != "" || doc.Command.Effects != "" {
		t.Fatal("inferred safe effects")
	}
}
