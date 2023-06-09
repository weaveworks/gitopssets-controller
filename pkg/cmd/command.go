package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/kubernetes"
	fakeclientgo "k8s.io/client-go/kubernetes/fake"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/pkg/setup"
)

// NewGenerateCommand creates and returns a new Command that renders GitOpsSets.
func NewGenerateCommand() *cobra.Command {
	var enabledGenerators []string
	var disableClusterAccess bool

	cmd := &cobra.Command{
		Use:   "generate [filename]",
		Short: "Render GitOpsSets from the CLI",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scheme, err := setup.NewSchemeForGenerators(enabledGenerators)
			if err != nil {
				return err
			}

			gitOpsSet, err := readFileAsGitOpsSet(scheme, args[0])
			if err != nil {
				return err
			}

			logger, err := newLogger()
			if err != nil {
				return err
			}

			services, cl, err := makeClients(disableClusterAccess, scheme)
			if err != nil {
				return err
			}

			factories := setup.GetGenerators(enabledGenerators, NewProxyArchiveFetcher(services), http.DefaultClient)
			gens := instantiateGenerators(factories, logger, cl)
			generated, err := templates.Render(context.Background(), gitOpsSet, gens)
			if err != nil {
				return err
			}

			return outputResources(generated)
		},
	}

	cmd.Flags().StringSliceVar(&enabledGenerators, "enabled-generators", setup.DefaultGenerators, "Generators to enable")
	cmd.Flags().BoolVar(&disableClusterAccess, "disable-cluster-access", false, "Disable cluster access - no access to Cluster resources will occur")

	return cmd
}

func makeClients(fakeClients bool, scheme *runtime.Scheme) (corev1.ServicesGetter, client.Client, error) {
	if fakeClients {
		return fakeclientgo.NewSimpleClientset().CoreV1(), fake.NewClientBuilder().WithScheme(scheme).Build(), nil
	}

	cfg, err := config.GetConfig()
	if err != nil {
		return nil, nil, err
	}

	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, nil, err
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, err
	}

	return clientset.CoreV1(), cl, nil

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

func instantiateGenerators(factories map[string]generators.GeneratorFactory, log logr.Logger, cl client.Client) map[string]generators.Generator {
	instantiatedGenerators := map[string]generators.Generator{}
	for k, factory := range factories {
		instantiatedGenerators[k] = factory(log, cl)
	}

	return instantiatedGenerators
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
		return nil, err
	}

	return bytesToGitOpsSet(scheme, b)
}

func bytesToGitOpsSet(scheme *runtime.Scheme, b []byte) (*templatesv1.GitOpsSet, error) {
	m, _, err := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(b, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decode rendered template: %w", err)
	}

	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(m)
	if err != nil {
		return nil, err
	}

	u := &unstructured.Unstructured{Object: raw}
	newObj, err := scheme.New(u.GetObjectKind().GroupVersionKind())
	if err != nil {
		return nil, err
	}

	return newObj.(*templatesv1.GitOpsSet), scheme.Convert(u, newObj, nil)
}
