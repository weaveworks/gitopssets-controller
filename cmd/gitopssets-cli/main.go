package main

import (
	"github.com/gitops-tools/gitopssets-controller/pkg/cmd"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "gitopssets-cli",
		Short: "GitOpsSets CLI",
	}

	rootCmd.AddCommand(cmd.NewGenerateCommand("generate"))
	cobra.CheckErr(rootCmd.Execute())
}
