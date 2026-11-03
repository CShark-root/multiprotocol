// Command agent will run on remote VPN servers and talk to the panel over
// gRPC + mTLS (PLAN.md phase 3). Until then the agent runs embedded in the
// panel process.
package main

import (
	"fmt"
	"os"

	"panel/internal/shared/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version.Version)
		return
	}
	fmt.Fprintln(os.Stderr, "standalone agent is not implemented yet; the panel runs an embedded agent")
	os.Exit(2)
}
