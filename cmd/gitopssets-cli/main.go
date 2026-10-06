package main

import (
	"github.com/gitops-tools/gitopssets-controller/pkg/cmd"
	"github.com/spf13/cobra"
)

// version is set with -X main.version at release build time.
var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:     "gitopssets-cli",
		Short:   "GitOpsSets CLI",
		Version: version,
	}

	rootCmd.AddCommand(cmd.NewGenerateCommand("generate"))
	cobra.CheckErr(rootCmd.Execute())
}
