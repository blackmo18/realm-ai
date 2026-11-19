package cli

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

func uiCmd(a *app) *cobra.Command {
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open the Realm web UI (served by the daemon on 127.0.0.1)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			url, err := c.StartUI(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintln(a.out, "Realm UI:", url)
			fmt.Fprintln(a.out, "The link carries a private token; do not share it.")
			if noOpen {
				return nil
			}
			if err := openBrowser(url); err != nil {
				fmt.Fprintln(a.errOut, "could not open a browser:", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "print the URL without opening a browser")
	return cmd
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
