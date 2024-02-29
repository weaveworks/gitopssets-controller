package cmd

import (
	"bytes"
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
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/kubernetes"
	fakeclientgo "k8s.io/client-go/kubernetes/fake"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators/apiclient"
	"github.com/gitops-tools/gitopssets-controller/pkg/parser"
	"github.com/gitops-tools/gitopssets-controller/pkg/setup"
)

// NewGenerateCommand creates and returns a new Command that renders GitOpsSets.
func NewGenerateCommand(name string) *cobra.Command {
	var enabledGenerators []string
	var disableClusterAccess bool
	var repositoryRoot string

	cmd := &cobra.Command{
		Use:   fmt.Sprintf("%s [filename]", name),
		Short: "Render GitOpsSet from the CLI",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderGitOpsSet(args[0], enabledGenerators, disableClusterAccess, repositoryRoot, os.Stdout)
		},
	}

	cmd.Flags().StringSliceVar(&enabledGenerators, "enabled-generators", setup.DefaultGenerators, "Generators to enable")
	cmd.Flags().BoolVarP(&disableClusterAccess, "disable-cluster-access", "d", false, "Disable cluster access - no access to Cluster resources will occur")
	cmd.Flags().StringVar(&repositoryRoot, "repository-root", "", "When cluster access is disabled GitRepository content is sourced relative to this path with the name of the GitRepository i.e. <repository-root>/<name of GitRepository>")

	return cmd
}

func makeClients(fakeClients bool, repositoryRoot string, scheme *runtime.Scheme, logger logr.Logger) (corev1.ServicesGetter, client.Reader, error) {
	if fakeClients {
		if repositoryRoot != "" {
			return fakeclientgo.NewSimpleClientset().CoreV1(), localObjectReader{repositoryRoot: repositoryRoot, logger: logger}, nil
		}

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

func outputResources(out io.Writer, resources []*unstructured.Unstructured) error {
	for _, r := range resources {
		if _, err := fmt.Fprintln(out, "---"); err != nil {
			return err
		}
		if err := marshalOutput(out, r); err != nil {
			return err
		}
	}

	return nil
}

func marshalOutput(out io.Writer, obj runtime.Object) error {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	_, err = fmt.Fprintf(out, "%s", data)
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

func instantiateGenerators(factories map[string]generators.GeneratorFactory, log logr.Logger, cl client.Reader) map[string]generators.Generator {
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

func readFileAsGitOpsSet(scheme *runtime.Scheme, filename string) ([]*templatesv1.GitOpsSet, error) {
	b, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	result := []*templatesv1.GitOpsSet{}

	for _, doc := range bytes.Split(b, []byte("---")) {
		if t := bytes.TrimSpace(doc); len(t) > 0 {
			set, err := bytesToGitOpsSet(scheme, doc)
			if err != nil {
				return nil, err
			}

			result = append(result, set)
		}
	}

	return result, nil
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

func renderGitOpsSet(filename string, enabledGenerators []string, disableClusterAccess bool, repositoryRoot string, out io.Writer) error {
	scheme, err := setup.NewSchemeForGenerators(enabledGenerators)
	if err != nil {
		return err
	}

	gitOpsSets, err := readFileAsGitOpsSet(scheme, filename)
	if err != nil {
		return err
	}

	logger, err := newLogger()
	if err != nil {
		return err
	}

	services, cl, err := makeClients(disableClusterAccess, repositoryRoot, scheme, logger)
	if err != nil {
		return err
	}

	var fetcher parser.ArchiveFetcher = NewProxyArchiveFetcher(services)
	if repositoryRoot != "" {
		fetcher = localFetcher{logger: logger}
	}

	factories := setup.GetGenerators(enabledGenerators, fetcher, apiclient.DefaultClientFactory)
	gens := instantiateGenerators(factories, logger, cl)

	var generated []*unstructured.Unstructured

	for _, set := range gitOpsSets {
		rendered, err := templates.Render(context.Background(), set, gens)
		if err != nil {
			return err
		}

		generated = append(generated, rendered...)
	}

	return outputResources(out, generated)
}
