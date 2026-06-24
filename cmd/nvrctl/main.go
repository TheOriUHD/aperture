// Command nvrctl is the Aperture NVR admin console. Run it with no arguments to
// open the full-screen TUI (login + deep configuration); the subcommands offer
// quick scriptable read-outs using the local CLI token.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/TheOriUHD/aperture/internal/version"
)

var (
	flagURL   string
	flagToken string
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// client builds a token-authed client for the scripting subcommands.
func client() (*Client, error) {
	token := resolveToken(flagToken)
	if token == "" {
		return nil, fmt.Errorf("no CLI token found — pass --token or run where data/cli.token is readable")
	}
	return newClient(flagURL, token), nil
}

// runConsole launches the interactive TUI. It gates on a login (or a still-valid
// saved session) rather than the CLI token.
func runConsole() error {
	c := newClient(flagURL, "")
	if s := loadSession(); s != nil && s.URL == c.BaseURL() {
		c.SetSession(s.Token)
	}
	return runTUI(c)
}

func main() {
	root := &cobra.Command{
		Use:           "nvrctl",
		Short:         "Aperture NVR admin console (TUI) + quick CLI read-outs",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		// No subcommand → open the console.
		RunE: func(_ *cobra.Command, _ []string) error { return runConsole() },
	}
	root.PersistentFlags().StringVar(&flagURL, "url", envOr("APERTURE_URL", "http://localhost:8971"), "nvrd API base URL")
	root.PersistentFlags().StringVar(&flagToken, "token", "", "CLI token (default: read from cli.token)")

	tui := &cobra.Command{
		Use:   "tui",
		Short: "Open the full-screen admin console",
		RunE:  func(_ *cobra.Command, _ []string) error { return runConsole() },
	}

	root.AddCommand(tui, statusCmd(), camerasCmd(), usersCmd(), storageCmd(), eventsCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
