// Command nvrctl is the Aperture NVR admin CLI/TUI. It talks to a running nvrd
// over its HTTP API, authenticating with the local CLI token (data/cli.token).
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

func client() (*Client, error) {
	token := resolveToken(flagToken)
	if token == "" {
		return nil, fmt.Errorf("no CLI token found — pass --token or run where data/cli.token is readable")
	}
	return newClient(flagURL, token), nil
}

func main() {
	root := &cobra.Command{
		Use:           "nvrctl",
		Short:         "Aperture NVR admin CLI/TUI",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&flagURL, "url", envOr("APERTURE_URL", "http://localhost:8080"), "nvrd API base URL")
	root.PersistentFlags().StringVar(&flagToken, "token", "", "CLI token (default: read from cli.token)")

	root.AddCommand(statusCmd(), camerasCmd(), usersCmd(), storageCmd(), eventsCmd(), tuiCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
