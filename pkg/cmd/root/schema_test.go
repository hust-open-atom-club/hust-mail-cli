package root

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/commandschema"
	"github.com/spf13/cobra"
)

func TestSchemaNeedsNoConfigurationOrCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HUST_MAIL_USER", "private-user-marker")
	t.Setenv("HUST_MAIL_PASSWORD", "private-password-marker")
	bad := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(bad, []byte("invalid JSON private-config-marker"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range [][]string{nil, {"send"}, {"config", "init"}, {"attachments", "export"}, {"hust-mail-cli"}, {"sync"}, {"schema"}, {"completion", "zsh"}} {
		var out bytes.Buffer
		cmd := NewCmdRoot(&out, io.Discard)
		// If description invokes any target business logic this test must fail.
		var guard func(*cobra.Command)
		guard = func(c *cobra.Command) {
			for _, child := range c.Commands() {
				if child.Name() != "schema" {
					child.RunE = func(*cobra.Command, []string) error { t.Fatal("target command executed"); return nil }
					guard(child)
				}
			}
		}
		guard(cmd)
		cmd.SetArgs(append([]string{"schema", "--config", bad, "--json"}, path...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("schema %v: %v", path, err)
		}
		var doc commandschema.Document
		if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if doc.FormatVersion != commandschema.FormatVersion {
			t.Fatal("missing format version")
		}
		for _, secret := range []string{"private-user-marker", "private-password-marker", "private-config-marker", bad} {
			if strings.Contains(out.String(), secret) {
				t.Fatal("private data exposed")
			}
		}
	}
}

func TestSchemaBusinessContracts(t *testing.T) {
	root := NewCmdRoot(io.Discard, io.Discard)
	for _, tc := range []struct {
		path                          []string
		effects, network, interactive string
		min, max                      int
	}{
		{[]string{"send"}, "smtpSend", "smtp", "never", 0, 0},
		{[]string{"doctor"}, "none", "imap,smtp", "never", 0, 0},
		{[]string{"config", "init"}, "localConfigCreate", "none", "required", 0, 0},
		{[]string{"read"}, "none", "none", "never", 1, 1},
		{[]string{"search"}, "localCacheWriteUnlessNoIndex", "none", "never", 0, 1},
		{[]string{"attachments", "export"}, "localAttachmentWrite", "none", "never", 1, 1},
	} {
		doc, err := commandschema.Describe(root, tc.path)
		if err != nil {
			t.Fatal(err)
		}
		c := doc.Command
		if c.Effects != tc.effects || c.Network != tc.network || c.Interactive != tc.interactive || c.Positionals.MinCount != tc.min || c.Positionals.MaxCount != tc.max {
			t.Fatalf("incorrect contract for %v", tc.path)
		}
	}
	doc, _ := commandschema.Describe(root, []string{"send"})
	required := []string{}
	for _, f := range doc.Command.Flags {
		if f.Required {
			required = append(required, f.Name)
		}
	}
	if !reflect.DeepEqual(required, []string{"subject", "to"}) {
		t.Fatalf("required flags: %v", required)
	}
	if len(doc.Command.FlagGroups) != 2 {
		t.Fatal("send exactly-one body constraint missing")
	}
	read, _ := commandschema.Describe(root, []string{"read"})
	if len(read.Command.FlagGroups) != 1 || !reflect.DeepEqual(read.Command.FlagGroups[0].Flags, []string{"json", "raw"}) {
		t.Fatal("raw/json incompatibility missing")
	}
	listed, _ := commandschema.Describe(root, nil)
	paths := map[string]bool{}
	for _, cmd := range listed.Commands {
		paths[cmd.Path] = true
	}
	for _, path := range []string{"hust-mail-cli schema", "hust-mail-cli config init", "hust-mail-cli attachments export", "hust-mail-cli completion zsh"} {
		if !paths[path] {
			t.Fatalf("missing %s", path)
		}
	}
}

func TestSchemaUnknownPathFailsWithoutPartialOutput(t *testing.T) {
	var out bytes.Buffer
	cmd := NewCmdRoot(&out, io.Discard)
	cmd.SetArgs([]string{"schema", "sy"})
	if err := cmd.Execute(); err == nil || out.Len() != 0 {
		t.Fatal("unknown path accepted or partial output produced")
	}
}
