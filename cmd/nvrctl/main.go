// Command nvrctl is the Aperture admin CLI/TUI. The full cobra command tree and
// the Bubble Tea dashboard land in a later phase; this is a minimal entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/TheOriUHD/aperture/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println("nvrctl", version.String())
			return
		}
	}
	fmt.Println("nvrctl", version.String())
	fmt.Println("the admin CLI/TUI is implemented in a later phase")
}
