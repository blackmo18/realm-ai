package cli

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/blackmo18/realm-ai/internal/client"
	"github.com/blackmo18/realm-ai/internal/tui"
)

var _ tui.API = (*client.Client)(nil)

func tuiCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the Realm terminal UI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope, err := a.scope(false)
			if err != nil {
				return err
			}
			c, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			p := tea.NewProgram(tui.New(cmd.Context(), c, scope), tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}
}
