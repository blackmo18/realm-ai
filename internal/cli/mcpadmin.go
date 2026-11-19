package cli

import (
	"github.com/spf13/cobra"

	"github.com/blackmo18/realm-ai/internal/adminmcp"
	"github.com/blackmo18/realm-ai/internal/mcpserver"
)

func mcpAdminCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp-admin",
		Short: "Serve the realm-admin MCP server on stdio (register with: claude mcp add realm -- realm mcp-admin)",
		Long: "realm-admin lets Claude Code or Claude Desktop manage Realm as you: projects, bots, knowledge, chats and DMs.\n" +
			"It talks to the realm daemon and starts it if needed. Register it with:\n\n  claude mcp add realm -- realm mcp-admin",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			srv := mcpserver.Server{Name: "realm-admin", Version: Version, Tools: adminmcp.Tools(), Call: adminmcp.Call(c)}
			return srv.Serve(cmd.Context(), a.in, a.out)
		},
	}
}
