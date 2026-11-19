// Command realm runs local Grok-style bots for Claude.
package main

import (
	"os"

	"github.com/blackmo18/realm-ai/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
