package main

import (
	"github.com/spf13/cobra"
	"github.com/weaveworks/gitopssets-controller/pkg/cmd"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "gitopssets-cli",
		Short: "GitOpsSets CLI",
	}

	rootCmd.AddCommand(cmd.NewGenerateCommand())
	cobra.CheckErr(rootCmd.Execute())
}
