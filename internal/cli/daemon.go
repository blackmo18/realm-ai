package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/blackmo18/realm-ai/internal/client"
	"github.com/blackmo18/realm-ai/internal/daemon"
	"github.com/blackmo18/realm-ai/internal/engine"
)

func daemonCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "daemon", Short: "Run or control the realm daemon"}
	cmd.AddCommand(
		&cobra.Command{Use: "run", Short: "Run the daemon in the foreground", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return runDaemon(cmd.Context(), a) }},
		&cobra.Command{Use: "start", Short: "Start the daemon in the background", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				c := client.New(a.socket())
				if c.Health(cmd.Context()) == nil {
					fmt.Fprintln(a.out, "daemon already running")
					return nil
				}
				if err := startDaemon(a); err != nil {
					return err
				}
				if err := c.WaitHealthy(cmd.Context(), 10_000_000_000); err != nil {
					return err
				}
				fmt.Fprintln(a.out, "daemon started")
				return nil
			}},
		&cobra.Command{Use: "stop", Short: "Stop the daemon", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if err := client.New(a.socket()).Shutdown(cmd.Context()); err != nil {
					return err
				}
				fmt.Fprintln(a.out, "daemon stopping")
				return nil
			}},
		&cobra.Command{Use: "status", Short: "Report whether the daemon runs", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if err := client.New(a.socket()).Health(cmd.Context()); err != nil {
					fmt.Fprintln(a.out, "stopped")
					return nil
				}
				fmt.Fprintln(a.out, "running on", a.socket())
				return nil
			}},
	)
	return cmd
}

func runDaemon(ctx context.Context, a *app) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	e, err := engine.New(engine.Options{Config: a.cfg, Global: a.global, SelfBin: self, Socket: a.socket()})
	if err != nil {
		return err
	}
	defer e.Close()
	ln, err := daemon.Listen(a.socket())
	if err != nil {
		return err
	}
	pidFile := filepath.Join(a.global.Root, "realm.pid")
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644)
	defer os.Remove(pidFile)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(a.errOut, "realm daemon listening on %s\n", a.socket())
	return daemon.Serve(ctx, ln, e)
}

func startDaemon(a *app) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.global.Root, 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(a.global.Root, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(self, "daemon", "run")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	return cmd.Process.Release()
}
