// Package cli wires the realm command line.
package cli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/client"
	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/engine"
	"github.com/blackmo18/realm-ai/internal/store"
)

// Version is set at build time.
var Version = "dev"

type app struct {
	global config.Scope
	cfg    config.Config
	out    io.Writer
	errOut io.Writer
	in     io.Reader
}

// maxSocketPath stays under the macOS unix socket path limit (104 bytes).
const maxSocketPath = 100

// socket is <home>/realm.sock, or a per-home path in the temp folder when
// the home path is too long for a unix socket.
func (a *app) socket() string {
	p := filepath.Join(a.global.Root, "realm.sock")
	if len(p) < maxSocketPath {
		return p
	}
	sum := sha256.Sum256([]byte(a.global.Root))
	return filepath.Join(os.TempDir(), fmt.Sprintf("realm-%x.sock", sum[:6]))
}

// local opens an engine for registry and bot-file operations. It never
// runs bot turns; the daemon does that.
func (a *app) local() (*engine.Engine, error) {
	return engine.New(engine.Options{Config: a.cfg, Global: a.global, Logf: func(string, ...any) {}})
}

// scope returns "global" with --global, else the project enclosing the
// working directory, else global.
func (a *app) scope(global bool) (string, error) {
	if global {
		return config.GlobalScope, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if _, err := config.FindProjectRoot(cwd); errors.Is(err, config.ErrNoProject) {
		return config.GlobalScope, nil
	}
	e, err := a.local()
	if err != nil {
		return "", err
	}
	defer e.Close()
	sc, err := e.ProjectForDir(cwd)
	if err != nil {
		return "", err
	}
	return sc.Name, nil
}

// client returns a daemon client, starting the daemon if needed.
func (a *app) client(ctx context.Context) (*client.Client, error) {
	c := client.New(a.socket())
	if err := c.Health(ctx); err == nil {
		return c, nil
	}
	if err := startDaemon(a); err != nil {
		return nil, err
	}
	if err := c.WaitHealthy(ctx, 10*time.Second); err != nil {
		return nil, fmt.Errorf("daemon did not start (see %s): %w", filepath.Join(a.global.Root, "daemon.log"), err)
	}
	return c, nil
}

// notify tells a running daemon about a bot change; it never starts one.
func (a *app) notify(ctx context.Context, handle string) {
	c := client.New(a.socket())
	if n, err := c.BotChanged(ctx, handle); err == nil && n > 0 {
		fmt.Fprintf(a.out, "dispatched %d queued message(s) to %s\n", n, handle)
	}
}

func (a *app) chatID(name string, global bool) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	scope, err := a.scope(global)
	if err != nil {
		return "", err
	}
	return engine.ChatID(scope, strings.TrimPrefix(name, "#")), nil
}

func (a *app) resolveBot(handle string, global bool) (bot.Bot, error) {
	scope, err := a.scope(global)
	if err != nil {
		return bot.Bot{}, err
	}
	e, err := a.local()
	if err != nil {
		return bot.Bot{}, err
	}
	defer e.Close()
	return e.ResolveBot(handle, scope)
}

func printMessage(w io.Writer, m store.Message) {
	ts := m.TS.Local().Format("15:04")
	switch m.Kind {
	case store.MsgSystem:
		fmt.Fprintf(w, "%s  · %s\n", ts, m.Text)
	case store.MsgQuestion:
		fmt.Fprintf(w, "%s  %s asks [%s]: %s\n", ts, m.From, m.ID, m.Text)
	case store.MsgText:
		fmt.Fprintf(w, "%s  %s [%s]: %s\n", ts, m.From, m.ID, m.Text)
	default:
		fmt.Fprintf(w, "%s  %s (%s): %s\n", ts, m.From, m.Kind, m.Text)
	}
}

// NewRoot builds the command tree.
func NewRoot(out, errOut io.Writer, in io.Reader) *cobra.Command {
	a := &app{out: out, errOut: errOut, in: in}
	root := &cobra.Command{
		Use:           "realm",
		Short:         "Local Grok-style bots for Claude",
		Long:          "Realm runs local bots on your Claude subscription (claude CLI) or on API keys.\nBots live globally (~/.realm) or per project (<project>/.realm).",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			g, err := config.Global()
			if err != nil {
				return err
			}
			cfg, err := config.Load(config.Path(g.Root))
			if err != nil {
				return err
			}
			a.global, a.cfg = g, cfg
			return nil
		},
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.AddCommand(initCmd(a), statusCmd(a), daemonCmd(a), botCmd(a), knowledgeCmd(a), chatCmd(a), dmCmd(a), uiCmd(a), tuiCmd(a), mcpCmd(a), mcpAdminCmd(a))
	return root
}

// Execute runs the CLI.
func Execute() int {
	root := NewRoot(os.Stdout, os.Stderr, os.Stdin)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func initCmd(a *app) *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up the global realm home, or register the current folder as a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := config.WriteDefault(config.Path(a.global.Root)); err != nil {
				return err
			}
			e, err := a.local()
			if err != nil {
				return err
			}
			defer e.Close()
			if global {
				fmt.Fprintf(a.out, "global realm ready at %s\n", a.global.Root)
				return nil
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			sc, err := e.RegisterProject(cwd)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "project %q ready at %s\nbots here are addressed as @%s/<name>\n", sc.Name, sc.Root, sc.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "only set up the global realm home")
	return cmd
}

func statusCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current scope, its bots and whether the daemon runs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope, err := a.scope(false)
			if err != nil {
				return err
			}
			running := client.New(a.socket()).Health(cmd.Context()) == nil
			fmt.Fprintf(a.out, "scope:  %s\nhome:   %s\ndaemon: %s\n", scope, a.global.Root, map[bool]string{true: "running", false: "stopped"}[running])
			e, err := a.local()
			if err != nil {
				return err
			}
			defer e.Close()
			bots, err := e.VisibleBots(scope)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "bots:   %d visible\n", len(bots))
			return nil
		},
	}
}
