package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"

	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/gitrepository"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/list"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/matrix"
)

func main() {
	cobra.CheckErr(makeRootCmd().Execute())
}

func makeRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gitopssets-cli [filename]",
		Short: "Render GitOpsSets from the CLI",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scheme, err := buildScheme()
			if err != nil {
				return err
			}

			gitOpsSet, err := readFileAsGitOpsSet(scheme, args[0])
			if err != nil {
				// TODO: improve error
				return err
			}

			cfg, err := config.GetConfig()
			if err != nil {
				return err
			}

			logger, err := newLogger()
			if err != nil {
				return err
			}

			cl, err := client.New(cfg, client.Options{Scheme: scheme})
			if err != nil {
				return err
			}

			gens := instantiateGenerators(logger, cl)
			generated, err := templates.Render(context.Background(), gitOpsSet, gens)
			if err != nil {
				// TODO: improve error
				return err
			}

			return outputResources(generated)
		},
	}

	return cmd
}

func outputResources(resources []*unstructured.Unstructured) error {
	for _, r := range resources {
		if _, err := fmt.Fprintln(os.Stdout, "---"); err != nil {
			return err
		}
		if err := marshalOutput(os.Stdout, r); err != nil {
			return err
		}
	}

	return nil
}

func marshalOutput(out io.Writer, output runtime.Object) error {
	data, err := yaml.Marshal(output)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %v", err)
	}

	_, err = fmt.Fprintf(out, "%s", data)
	if err != nil {
		return fmt.Errorf("failed to write data: %v", err)
	}

	return nil
}

// TODO: rework this to accept the configured generators.
func instantiateGenerators(log logr.Logger, cl client.Client) map[string]generators.Generator {
	instantiatedGenerators := map[string]generators.Generator{}
	for k, factory := range configuredGenerators() {
		instantiatedGenerators[k] = factory(log, cl)
	}

	return instantiatedGenerators
}

func configuredGenerators() map[string]generators.GeneratorFactory {
	matrixGenerators := map[string]generators.GeneratorFactory{
		"List":          list.GeneratorFactory,
		"GitRepository": gitrepository.GeneratorFactory,
	}

	return map[string]generators.GeneratorFactory{
		"List":          list.GeneratorFactory,
		"GitRepository": gitrepository.GeneratorFactory,
		"Matrix":        matrix.GeneratorFactory(matrixGenerators),
	}
}

func newLogger() (logr.Logger, error) {
	cfg := zap.NewDevelopmentConfig()
	cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapLog, err := cfg.Build()
	if err != nil {
		return logr.Discard(), err
	}

	return zapr.NewLogger(zapLog), nil
}

func readFileAsGitOpsSet(scheme *runtime.Scheme, filename string) (*templatesv1.GitOpsSet, error) {
	b, err := os.ReadFile(filename)
	if err != nil {
		// TODO: improve error
		return nil, err
	}

	return bytesToGitOpsSet(scheme, b)
}
