package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/knowledge"
)

type botFlags struct {
	provider, model, effort, charter string
	topics, skills, tools            []string
}

func (f *botFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.provider, "provider", "", "claude-cli (subscription) or a provider name from config.yaml")
	cmd.Flags().StringVar(&f.model, "model", "", "model, e.g. haiku, sonnet, opus, or a provider model id")
	cmd.Flags().StringVar(&f.effort, "effort", "", "low, medium, high, xhigh or max")
	cmd.Flags().StringVar(&f.charter, "charter", "", "the bot's scope: the only work it acts on")
	cmd.Flags().StringSliceVar(&f.topics, "topics", nil, "comma-separated topics, used for routing suggestions")
	cmd.Flags().StringSliceVar(&f.skills, "skills", nil, `allowed skills; "*" for all at the bot's scope`)
	cmd.Flags().StringSliceVar(&f.tools, "tools", nil, "allowed claude-cli built-in tools, e.g. Read,Grep,Glob,Edit")
}

// apply returns a copy of b with every flag the user set.
func (f *botFlags) apply(cmd *cobra.Command, b bot.Bot) bot.Bot {
	out := b
	set := func(name string) bool { return cmd.Flags().Changed(name) }
	if set("provider") {
		out.Provider = f.provider
	}
	if set("model") {
		out.Model = f.model
	}
	if set("effort") {
		out.Effort = f.effort
	}
	if set("charter") {
		out.Charter = f.charter
	}
	if set("topics") {
		out.Topics = f.topics
	}
	if set("skills") {
		out.Skills = f.skills
	}
	if set("tools") {
		out.Tools = f.tools
	}
	return out
}

func botCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "bot", Short: "Create and manage bots"}
	cmd.AddCommand(botCreateCmd(a), botListCmd(a), botShowCmd(a), botSetCmd(a), botInheritCmd(a))
	for _, action := range []string{bot.ActionOn, bot.ActionOff, bot.ActionArchive, bot.ActionRestore} {
		cmd.AddCommand(botLifecycleCmd(a, action))
	}
	return cmd
}

func botCreateCmd(a *app) *cobra.Command {
	var f botFlags
	var global bool
	var inherit string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a bot in the current project (or globally with --global)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := a.scope(global)
			if err != nil {
				return err
			}
			e, err := a.local()
			if err != nil {
				return err
			}
			defer e.Close()
			draft := f.apply(cmd, bot.Bot{})
			in := engine.BotInput{Scope: scope, Name: args[0], Provider: draft.Provider, Model: draft.Model, Effort: draft.Effort,
				Charter: draft.Charter, Topics: draft.Topics, Skills: draft.Skills, Inherit: inherit}
			if cmd.Flags().Changed("tools") {
				in.Tools = nonNilTools(draft.Tools)
			}
			v, err := e.CreateBot(in)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "created %s (%s, model %s)\n", v.Handle, v.Provider, orDefault(v.Model, "default"))
			if inherit != "" {
				fmt.Fprintf(a.out, "%s inherited %d knowledge entries from %s\n", v.Handle, v.KnowledgeCount, inherit)
			}
			return nil
		},
	}
	f.register(cmd)
	cmd.Flags().BoolVar(&global, "global", false, "create a global bot")
	cmd.Flags().StringVar(&inherit, "inherit", "", "copy knowledge from an existing bot")
	_ = cmd.MarkFlagRequired("charter")
	return cmd
}

func nonNilTools(t []string) []string {
	if t == nil {
		return []string{}
	}
	return t
}

func botInheritCmd(a *app) *cobra.Command {
	var from string
	var global bool
	cmd := &cobra.Command{
		Use:   "inherit <bot> --from <source-bot>",
		Short: "Copy another bot's knowledge into an existing bot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := a.scope(global)
			if err != nil {
				return err
			}
			e, err := a.local()
			if err != nil {
				return err
			}
			defer e.Close()
			dst, err := e.ResolveBot(args[0], scope)
			if err != nil {
				return err
			}
			src, err := e.ResolveBot(from, scope)
			if err != nil {
				return err
			}
			n, err := e.InheritKnowledge(dst.Handle(), src.Handle())
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%s inherited %d knowledge entries from %s\n", dst.Handle(), n, src.Handle())
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "bot to copy knowledge from")
	cmd.Flags().BoolVar(&global, "global", false, "resolve names in the global scope")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

func botListCmd(a *app) *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List bots visible from the current scope",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope, err := a.scope(global)
			if err != nil {
				return err
			}
			e, err := a.local()
			if err != nil {
				return err
			}
			defer e.Close()
			bots, err := e.VisibleBots(scope)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(a.out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "HANDLE\tSTATUS\tPROVIDER\tMODEL\tCHARTER")
			for _, b := range bots {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", b.Handle(), b.Status, b.Provider, orDefault(b.Model, "-"), clip(b.FirstLine(), 60))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "list from the global scope (shows every project's bots)")
	return cmd
}

func botShowCmd(a *app) *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "show <bot>",
		Short: "Show a bot's definition and knowledge index",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.resolveBot(args[0], global)
			if err != nil {
				return err
			}
			data, err := yaml.Marshal(b)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "# %s (%s)\n%s", b.Handle(), b.Dir, data)
			idx, _ := knowledge.Open(bot.KnowledgeDir(b)).Index()
			if idx != "" {
				fmt.Fprintf(a.out, "\nknowledge:\n%s", idx)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "resolve the name in the global scope")
	return cmd
}

func botSetCmd(a *app) *cobra.Command {
	var f botFlags
	var global bool
	cmd := &cobra.Command{
		Use:   "set <bot>",
		Short: "Change a bot's model, effort, provider, charter, topics, skills or tools",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.resolveBot(args[0], global)
			if err != nil {
				return err
			}
			e, err := a.local()
			if err != nil {
				return err
			}
			defer e.Close()
			repo, err := e.Repo(b.Scope)
			if err != nil {
				return err
			}
			updated, err := repo.Update(b.Name, func(cur bot.Bot) bot.Bot { return f.apply(cmd, cur) })
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "updated %s (%s, model %s, effort %s)\n", updated.Handle(), updated.Provider, orDefault(updated.Model, "default"), orDefault(updated.Effort, "default"))
			return nil
		},
	}
	f.register(cmd)
	cmd.Flags().BoolVar(&global, "global", false, "resolve the name in the global scope")
	return cmd
}

func botLifecycleCmd(a *app, action string) *cobra.Command {
	short := map[string]string{
		bot.ActionOn:      "Turn a bot on (queued messages are delivered)",
		bot.ActionOff:     "Turn a bot off (messages queue; knowledge is kept)",
		bot.ActionArchive: "Archive a bot (rejects messages; knowledge is kept)",
		bot.ActionRestore: "Restore an archived bot",
	}[action]
	var global bool
	cmd := &cobra.Command{
		Use:   action + " <bot>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.resolveBot(args[0], global)
			if err != nil {
				return err
			}
			e, err := a.local()
			if err != nil {
				return err
			}
			defer e.Close()
			repo, err := e.Repo(b.Scope)
			if err != nil {
				return err
			}
			updated, err := repo.Apply(b.Name, action)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%s is %s\n", updated.Handle(), updated.Status)
			a.notify(cmd.Context(), updated.Handle())
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "resolve the name in the global scope")
	return cmd
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
