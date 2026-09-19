// Command dbvault is the DBVault command-line interface.
package main

import (
	"os"

	"github.com/dbvault/dbvault/cli/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
