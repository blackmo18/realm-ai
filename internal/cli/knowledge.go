package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/knowledge"
)

func knowledgeCmd(a *app) *cobra.Command {
	var global bool
	open := func(handle string) (*knowledge.Store, bot.Bot, error) {
		b, err := a.resolveBot(handle, global)
		if err != nil {
			return nil, bot.Bot{}, err
		}
		return knowledge.Open(bot.KnowledgeDir(b)), b, nil
	}
	cmd := &cobra.Command{Use: "knowledge", Short: "Read and edit a bot's persistent knowledge"}
	cmd.PersistentFlags().BoolVar(&global, "global", false, "resolve the bot name in the global scope")

	list := &cobra.Command{Use: "list <bot>", Short: "List knowledge entries", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ks, b, err := open(args[0])
			if err != nil {
				return err
			}
			entries, err := ks.List()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Fprintf(a.out, "%s has no knowledge yet\n", b.Handle())
			}
			for _, en := range entries {
				origin := ""
				if en.Origin != "" {
					origin = " (from " + en.Origin + ")"
				}
				fmt.Fprintf(a.out, "%s  %s%s\n", en.ID, en.Title, origin)
			}
			return nil
		}}
	show := &cobra.Command{Use: "show <bot> <id>", Short: "Show one entry", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ks, _, err := open(args[0])
			if err != nil {
				return err
			}
			en, err := ks.Get(args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "# %s\n%s\n", en.Title, en.Body)
			return nil
		}}
	var title string
	add := &cobra.Command{Use: "add <bot> <text...> --title <title>", Short: "Add an entry", Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ks, _, err := open(args[0])
			if err != nil {
				return err
			}
			en, err := ks.Remember(title, strings.Join(args[1:], " "))
			if err != nil {
				return err
			}
			fmt.Fprintln(a.out, "saved", en.ID)
			return nil
		}}
	add.Flags().StringVar(&title, "title", "", "entry title")
	_ = add.MarkFlagRequired("title")
	forget := &cobra.Command{Use: "forget <bot> <id>", Short: "Delete an entry", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ks, _, err := open(args[0])
			if err != nil {
				return err
			}
			if err := ks.Forget(args[1]); err != nil {
				return err
			}
			fmt.Fprintln(a.out, "forgot", args[1])
			return nil
		}}
	cmd.AddCommand(list, show, add, forget)
	return cmd
}
