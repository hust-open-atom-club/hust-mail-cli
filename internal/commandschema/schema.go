// Package commandschema describes Cobra registrations without executing hooks,
// argument validators, completion functions, or command handlers.
package commandschema

import (
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const FormatVersion = 1
const annotationKey = "hust-mail-cli.command-description"

type Positionals struct {
	MinCount    int    `json:"minCount"`
	MaxCount    int    `json:"maxCount"` // -1 means unbounded
	Description string `json:"description"`
}

type FlagGroup struct {
	Kind  string   `json:"kind"`
	Flags []string `json:"flags"`
}

// Metadata records only static facts not available from Cobra. Keep annotations
// beside command registration; never include resolved config or runtime values.
type Metadata struct {
	Positionals *Positionals `json:"positionals,omitempty"`
	Output      string       `json:"output,omitempty"`
	Effects     string       `json:"effects,omitempty"`
	Network     string       `json:"network,omitempty"`
	Config      string       `json:"config,omitempty"`
	Interactive string       `json:"interactive,omitempty"`
	FlagGroups  []FlagGroup  `json:"flagGroups,omitempty"`
	Notes       []string     `json:"notes,omitempty"`
}

func Annotate(cmd *cobra.Command, m Metadata) {
	data, _ := json.Marshal(m)
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[annotationKey] = string(data)
}

type Document struct {
	FormatVersion int       `json:"formatVersion"`
	Commands      []Summary `json:"commands,omitempty"`
	Command       *Command  `json:"command,omitempty"`
}

type Summary struct {
	Path        string   `json:"path"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases"`
	Runnable    bool     `json:"runnable"`
	Deprecated  string   `json:"deprecated"`
}

type Flag struct {
	Name         string `json:"name"`
	Shorthand    string `json:"shorthand"`
	Description  string `json:"description"`
	Type         string `json:"type"`
	Default      string `json:"default"`
	NoOptDefault string `json:"noOptDefault"`
	Required     bool   `json:"required"`
	Inherited    bool   `json:"inherited"`
	DefinedOn    string `json:"definedOn"`
	Deprecated   string `json:"deprecated"`
}

type Command struct {
	Summary
	Usage           string      `json:"usage"`
	LongDescription string      `json:"longDescription"`
	Example         string      `json:"example"`
	Subcommands     []Summary   `json:"subcommands"`
	Flags           []Flag      `json:"flags"`
	FlagGroups      []FlagGroup `json:"flagGroups"`
	Metadata
	Validation  string   `json:"validation"`
	Undescribed []string `json:"undescribed"`
}

// Describe accepts exact canonical names or registered aliases, optionally
// prefixed with the executable name. Empty path lists all public commands.
func Describe(root *cobra.Command, path []string) (Document, error) {
	doc := Document{FormatVersion: FormatVersion}
	if root == nil {
		return doc, errors.New("command tree unavailable")
	}
	if len(path) == 0 {
		doc.Commands = []Summary{}
		var walk func(*cobra.Command)
		walk = func(cmd *cobra.Command) {
			if cmd.Hidden {
				return
			}
			doc.Commands = append(doc.Commands, summarize(cmd))
			for _, child := range cmd.Commands() {
				walk(child)
			}
		}
		walk(root)
		sort.Slice(doc.Commands, func(i, j int) bool { return doc.Commands[i].Path < doc.Commands[j].Path })
		return doc, nil
	}
	if path[0] == root.Name() {
		path = path[1:]
	}
	cmd := root
	for _, part := range path {
		var next *cobra.Command
		for _, child := range cmd.Commands() {
			if !child.Hidden && child.Name() == part {
				next = child
				break
			}
		}
		if next == nil {
			for _, child := range cmd.Commands() {
				if !child.Hidden && slices.Contains(child.Aliases, part) {
					next = child
					break
				}
			}
		}
		if next == nil {
			return doc, errors.New("unknown or hidden command path; run 'hust-mail-cli schema' to list public commands")
		}
		cmd = next
	}
	detail, err := describe(cmd)
	if err != nil {
		return doc, err
	}
	doc.Command = &detail
	return doc, nil
}

func summarize(cmd *cobra.Command) Summary {
	aliases := append([]string{}, cmd.Aliases...)
	sort.Strings(aliases)
	return Summary{Path: cmd.CommandPath(), Description: strings.TrimSpace(cmd.Short), Aliases: aliases, Runnable: cmd.Runnable(), Deprecated: cmd.Deprecated}
}

func describe(cmd *cobra.Command) (Command, error) {
	cmd.InitDefaultHelpFlag()
	m := Metadata{}
	if raw := cmd.Annotations[annotationKey]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return Command{}, errors.New("invalid static command metadata")
		}
	}
	inherited := cmd.InheritedFlags()
	flags := map[string]*pflag.Flag{}
	inherited.VisitAll(func(f *pflag.Flag) { flags[f.Name] = f })
	cmd.NonInheritedFlags().VisitAll(func(f *pflag.Flag) { flags[f.Name] = f })
	c := Command{Summary: summarize(cmd), Usage: cmd.UseLine(), LongDescription: strings.TrimSpace(cmd.Long), Example: strings.TrimSpace(cmd.Example), Subcommands: []Summary{}, Flags: []Flag{}, FlagGroups: []FlagGroup{}, Metadata: m, Validation: "partial", Undescribed: []string{"businessValidation", "responseSchema"}}
	if m.Positionals == nil {
		c.Undescribed = append(c.Undescribed, "positionals")
	}
	for name, value := range map[string]string{"output": m.Output, "effects": m.Effects, "network": m.Network, "config": m.Config, "interactive": m.Interactive} {
		if value == "" {
			c.Undescribed = append(c.Undescribed, name)
		}
	}
	sort.Strings(c.Undescribed)
	for _, child := range cmd.Commands() {
		if !child.Hidden {
			c.Subcommands = append(c.Subcommands, summarize(child))
		}
	}
	sort.Slice(c.Subcommands, func(i, j int) bool { return c.Subcommands[i].Path < c.Subcommands[j].Path })
	for _, f := range flags {
		if f.Hidden {
			continue
		}
		required := slices.Contains(f.Annotations[cobra.BashCompOneRequiredFlag], "true")
		isInherited := inherited.Lookup(f.Name) == f
		owner := cmd.CommandPath()
		if isInherited {
			for p := cmd.Parent(); p != nil; p = p.Parent() {
				if p.PersistentFlags().Lookup(f.Name) == f {
					owner = p.CommandPath()
					break
				}
			}
		}
		c.Flags = append(c.Flags, Flag{Name: f.Name, Shorthand: f.Shorthand, Description: f.Usage, Type: f.Value.Type(), Default: f.DefValue, NoOptDefault: f.NoOptDefVal, Required: required, Inherited: isInherited, DefinedOn: owner, Deprecated: f.Deprecated})
	}
	sort.Slice(c.Flags, func(i, j int) bool { return c.Flags[i].Name < c.Flags[j].Name })
	c.FlagGroups = groups(flags, m.FlagGroups)
	return c, nil
}

func groups(flags map[string]*pflag.Flag, extra []FlagGroup) []FlagGroup {
	result := []FlagGroup{}
	seen := map[string]bool{}
	add := func(g FlagGroup) {
		names := append([]string{}, g.Flags...)
		sort.Strings(names)
		if len(names) == 0 {
			return
		}
		for _, name := range names {
			if f := flags[name]; f == nil || f.Hidden {
				return
			}
		}
		key := g.Kind + ":" + strings.Join(names, " ")
		if !seen[key] {
			seen[key] = true
			result = append(result, FlagGroup{Kind: g.Kind, Flags: names})
		}
	}
	for _, g := range extra {
		add(g)
	}
	// Cobra v1.10 uses these annotations for flag relationships. Tests exercise
	// public MarkFlags* methods so a future annotation change is detected.
	for key, kind := range map[string]string{"cobra_annotation_required_if_others_set": "requiredTogether", "cobra_annotation_one_required": "atLeastOneRequired", "cobra_annotation_mutually_exclusive": "mutuallyExclusive"} {
		for _, f := range flags {
			if f.Hidden {
				continue
			}
			for _, raw := range f.Annotations[key] {
				add(FlagGroup{Kind: kind, Flags: strings.Fields(raw)})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Kind+strings.Join(result[i].Flags, " ") < result[j].Kind+strings.Join(result[j].Flags, " ")
	})
	return result
}
