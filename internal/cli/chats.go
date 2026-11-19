package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/client"
	"github.com/blackmo18/realm-ai/internal/daemon"
	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/mcpserver"
	"github.com/blackmo18/realm-ai/internal/store"
	"github.com/blackmo18/realm-ai/internal/tools"
)

func chatCmd(a *app) *cobra.Command {
	var global bool
	cmd := &cobra.Command{Use: "chat", Short: "Group chats with bots"}
	cmd.PersistentFlags().BoolVar(&global, "global", false, "use the global scope instead of the current project")

	withChat := func(fn func(ctx context.Context, c *client.Client, chatID string, args []string) error) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			chatID, err := a.chatID(args[0], global)
			if err != nil {
				return err
			}
			c, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			return fn(cmd.Context(), c, chatID, args[1:])
		}
	}

	var routing string
	var members []string
	create := &cobra.Command{Use: "create <name>", Short: "Create a group chat", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := a.scope(global)
			if err != nil {
				return err
			}
			c, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			chat, err := c.CreateChat(cmd.Context(), daemon.ChatCreate{Scope: scope, Name: args[0], Routing: routing, Members: members})
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "created %s (routing: %s)\n", chat.ID, chat.Routing)
			return nil
		}}
	create.Flags().StringVar(&routing, "routing", store.RoutingModerated, "moderated (moderator routes un-mentioned messages) or mention (only @mentions reach bots)")
	create.Flags().StringSliceVar(&members, "members", nil, "extra bots to add, e.g. global bots in a project chat")

	list := &cobra.Command{Use: "list", Short: "List chats in the current scope", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope, err := a.scope(global)
			if err != nil {
				return err
			}
			c, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			chats, err := c.ListChats(cmd.Context(), scope)
			if err != nil {
				return err
			}
			for _, ch := range chats {
				fmt.Fprintf(a.out, "%s  [%s, %s]\n", ch.ID, ch.Kind, ch.Routing)
			}
			return nil
		}}

	post := &cobra.Command{Use: "post <chat> <text...>", Short: "Post a message (mention @bots, or let the moderator route it)", Args: cobra.MinimumNArgs(2),
		RunE: withChat(func(ctx context.Context, c *client.Client, chatID string, args []string) error {
			m, err := c.Post(ctx, chatID, strings.Join(args, " "))
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "posted %s (watch replies with: realm chat tail %s)\n", m.ID, chatID)
			return nil
		})}

	var history int
	tail := &cobra.Command{Use: "tail <chat>", Short: "Print recent messages and follow new ones", Args: cobra.ExactArgs(1),
		RunE: withChat(func(ctx context.Context, c *client.Client, chatID string, _ []string) error {
			return follow(ctx, a, c, chatID, history)
		})}
	tail.Flags().IntVar(&history, "history", 20, "recent messages to print first")

	join := &cobra.Command{Use: "join <chat>", Short: "Interactive chat: follow messages and type to post (/pick @bot, /reroute <id> @bot, /quit)", Args: cobra.ExactArgs(1),
		RunE: withChat(func(ctx context.Context, c *client.Client, chatID string, _ []string) error {
			return interactive(ctx, a, c, chatID)
		})}

	var askID string
	pick := &cobra.Command{Use: "pick <chat> <@bot>", Short: "Answer the moderator's question by picking a bot", Args: cobra.ExactArgs(2),
		RunE: withChat(func(ctx context.Context, c *client.Client, chatID string, args []string) error {
			target, err := c.Pick(ctx, chatID, askID, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(a.out, "routed to", target)
			return nil
		})}
	pick.Flags().StringVar(&askID, "ask", "", "question message id (default: latest)")

	reroute := &cobra.Command{Use: "reroute <chat> <message-id> <@bot>", Short: "Send a message to a different bot and teach the moderator", Args: cobra.ExactArgs(3),
		RunE: withChat(func(ctx context.Context, c *client.Client, chatID string, args []string) error {
			target, err := c.Reroute(ctx, chatID, args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintln(a.out, "rerouted to", target)
			return nil
		})}

	addMember := &cobra.Command{Use: "add <chat> <@bot>", Short: "Add a bot to a chat (e.g. a global bot to a project chat)", Args: cobra.ExactArgs(2),
		RunE: withChat(func(ctx context.Context, c *client.Client, chatID string, args []string) error {
			h, err := c.AddMember(ctx, chatID, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%s joined %s\n", h, chatID)
			return nil
		})}

	setRouting := &cobra.Command{Use: "routing <chat> <moderated|mention>", Short: "Switch chat routing", Args: cobra.ExactArgs(2),
		RunE: withChat(func(ctx context.Context, c *client.Client, chatID string, args []string) error {
			if err := c.SetRouting(ctx, chatID, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%s routing: %s\n", chatID, args[0])
			return nil
		})}

	cmd.AddCommand(create, list, post, tail, join, pick, reroute, addMember, setRouting)
	return cmd
}

func follow(ctx context.Context, a *app, c *client.Client, chatID string, history int) error {
	stream, err := c.Stream(ctx, chatID)
	if err != nil {
		return err
	}
	if history > 0 {
		msgs, err := c.Messages(ctx, chatID, history)
		if err != nil {
			return err
		}
		for _, m := range msgs {
			printMessage(a.out, m)
		}
	}
	for m := range stream {
		printMessage(a.out, m)
	}
	return nil
}

func interactive(ctx context.Context, a *app, c *client.Client, chatID string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = follow(ctx, a, c, chatID, 20) }()
	fmt.Fprintf(a.out, "joined %s — type a message, @mention a bot, /pick @bot, /reroute <id> @bot, /quit\n", chatID)
	sc := bufio.NewScanner(a.in)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		fields := strings.Fields(line)
		var err error
		switch {
		case line == "":
			continue
		case line == "/quit" || line == "/exit":
			return nil
		case len(fields) == 2 && fields[0] == "/pick":
			_, err = c.Pick(ctx, chatID, "", fields[1])
		case len(fields) == 3 && fields[0] == "/reroute":
			_, err = c.Reroute(ctx, chatID, fields[1], fields[2])
		default:
			_, err = c.Post(ctx, chatID, line)
		}
		if err != nil {
			fmt.Fprintln(a.errOut, "error:", err)
		}
	}
	return sc.Err()
}

func dmCmd(a *app) *cobra.Command {
	var global bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "dm <@bot> <text...>",
		Short: "Message a bot directly and print its reply",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.resolveBot(args[0], global)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			c, err := a.client(ctx)
			if err != nil {
				return err
			}
			chatID := engine.DMChatID(b.Scope, bot.UserHandle, b.Handle())
			stream, err := c.Stream(ctx, chatID)
			if err != nil && !strings.Contains(err.Error(), "not found") {
				return err
			}
			posted, err := c.DM(ctx, b.Handle(), b.Scope, strings.Join(args[1:], " "))
			if err != nil {
				return err
			}
			if stream == nil {
				if stream, err = c.Stream(ctx, posted.Chat); err != nil {
					return err
				}
				if replied := printExisting(ctx, a, c, posted); replied {
					return nil
				}
			}
			return awaitReply(ctx, a, stream, posted)
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "resolve the name in the global scope")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "how long to wait for the reply")
	return cmd
}

// printExisting covers the race where the reply landed before the stream
// connected on a brand-new DM chat.
func printExisting(ctx context.Context, a *app, c *client.Client, posted store.Message) bool {
	msgs, err := c.Messages(ctx, posted.Chat, 10)
	if err != nil {
		return false
	}
	for _, m := range msgs {
		if m.Parent == posted.ID {
			fmt.Fprintln(a.out, m.Text)
			return true
		}
	}
	return false
}

func awaitReply(ctx context.Context, a *app, stream <-chan store.Message, posted store.Message) error {
	for {
		select {
		case <-ctx.Done():
			return errors.New("timed out waiting for a reply (it will still appear in the chat)")
		case m, open := <-stream:
			if !open {
				return errors.New("daemon connection closed")
			}
			if m.Parent != posted.ID {
				continue
			}
			if m.Kind == store.MsgSystem {
				fmt.Fprintln(a.errOut, "·", m.Text)
			} else {
				fmt.Fprintln(a.out, m.Text)
			}
			return nil
		}
	}
}

func mcpCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:    "mcp",
		Short:  "Serve realm tools over MCP stdio (started by the daemon for claude-cli bots)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			token := os.Getenv("REALM_TURN_TOKEN")
			socket := os.Getenv("REALM_SOCKET")
			if token == "" || socket == "" {
				return errors.New("realm mcp is started by the realm daemon; REALM_TURN_TOKEN and REALM_SOCKET are required")
			}
			c := client.New(socket)
			srv := mcpserver.Server{Name: "realm", Version: Version, Tools: tools.Realm(), Call: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
				return c.Tool(ctx, token, name, args)
			}}
			return srv.Serve(cmd.Context(), a.in, a.out)
		},
	}
}
