package setup

import (
	"fmt"

	"golang.org/x/exp/slices"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/apiclient"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/cluster"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/gitrepository"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/gitrepository/parser"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/imagepolicy"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/list"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/matrix"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/pullrequests"
	//+kubebuilder:scaffold:imports
)

// AllGenerators contains the name of all possible Generators.
var AllGenerators = []string{"GitRepository", "Cluster", "PullRequests", "List", "APIClient", "ImagePolicy", "Matrix", "Config"}

// DefaultGenerators contains the name of the default set of enabled Generators,
// this leaves out generators that require optional dependencies.
var DefaultGenerators = []string{"GitRepository", "PullRequests", "List", "APIClient", "Matrix", "Config"}

// NewSchemeForGenerators creates and returns a runtime.Scheme configured with
// the correct schemes for the enabled generators.
func NewSchemeForGenerators(enabledGenerators []string) (*runtime.Scheme, error) {
	builder := runtime.SchemeBuilder{
		clientgoscheme.AddToScheme,
		sourcev1.AddToScheme,
		templatesv1.AddToScheme,
	}

	if isGeneratorEnabled(enabledGenerators, "Cluster") {
		builder.Register(clustersv1.AddToScheme)
	}

	if isGeneratorEnabled(enabledGenerators, "ImagePolicy") {
		builder.Register(imagev1.AddToScheme)
	}

	scheme := runtime.NewScheme()

	if err := builder.AddToScheme(scheme); err != nil {
		return nil, err
	}

	return scheme, nil
}

// ValidateEnabledGenerators returns an error if an invalid name is provided for
// the set of enabled generators.
//
// If all provided names are valid, no error is returned.
func ValidateEnabledGenerators(enabledGenerators []string) error {
	for _, generator := range enabledGenerators {
		if !slices.Contains(AllGenerators, generator) {
			return fmt.Errorf("invalid generator %q. valid values: %q", generator, AllGenerators)
		}
	}

	return nil
}

// GetGenenerators returns a set of generator factories for the set of enabled
// generators.
func GetGenerators(enabledGenerators []string, fetcher parser.ArchiveFetcher, clientFactory apiclient.HTTPClientFactory) map[string]generators.GeneratorFactory {
	matrixGenerators := filterEnabledGenerators(enabledGenerators, map[string]generators.GeneratorFactory{
		"List":          list.GeneratorFactory,
		"GitRepository": gitrepository.GeneratorFactory(fetcher),
		"PullRequests":  pullrequests.GeneratorFactory,
		"Cluster":       cluster.GeneratorFactory,
		"ImagePolicy":   imagepolicy.GeneratorFactory,
		"APIClient":     apiclient.GeneratorFactory(clientFactory),
	})

	return filterEnabledGenerators(enabledGenerators, map[string]generators.GeneratorFactory{
		"List":          list.GeneratorFactory,
		"GitRepository": gitrepository.GeneratorFactory(fetcher),
		"PullRequests":  pullrequests.GeneratorFactory,
		"Cluster":       cluster.GeneratorFactory,
		"APIClient":     apiclient.GeneratorFactory(clientFactory),
		"ImagePolicy":   imagepolicy.GeneratorFactory,
		"Matrix":        matrix.GeneratorFactory(matrixGenerators),
	})
}

func filterEnabledGenerators(enabledGenerators []string, gens map[string]generators.GeneratorFactory) map[string]generators.GeneratorFactory {
	newGenerators := make(map[string]generators.GeneratorFactory)
	for generatorName := range gens {
		if isGeneratorEnabled(enabledGenerators, generatorName) {
			newGenerators[generatorName] = gens[generatorName]
		}
	}
	return newGenerators
}

func isGeneratorEnabled(enabledGenerators []string, generatorName string) bool {
	return slices.Contains(enabledGenerators, generatorName)
}
