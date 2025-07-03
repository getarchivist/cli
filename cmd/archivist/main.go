package main

import (
	"fmt"
	"os"

	"github.com/getarchivist/archivist/cli/cmd/archivist/commands"
)

func main() {
	if err := commands.RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
