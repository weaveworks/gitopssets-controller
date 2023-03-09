package tests

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fluxcd/pkg/runtime/testenv"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	gitopssetsv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/cluster"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/gitrepository"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/list"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/matrix"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/pullrequests"
	// +kubebuilder:scaffold:imports
)

const (
	timeout = 10 * time.Second
)

var (
	testEnv *testenv.Environment
	ctx     = ctrl.SetupSignalHandler()
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func TestMain(m *testing.M) {
	utilruntime.Must(gitopssetsv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(clustersv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme.Scheme))
	testEnv = testenv.New(testenv.WithCRDPath(filepath.Join("..", "..", "config", "crd", "bases"),
		filepath.Join("..", "..", "controllers", "testdata", "crds"),
		filepath.Join("testdata", "crds"),
	))
	mapper, err := apiutil.NewDynamicRESTMapper(testEnv.GetConfig())
	if err != nil {
		panic(fmt.Sprintf("failed to create RESTMapper:  %v", err))
	}

	if err := (&controllers.GitOpsSetReconciler{
		Client: testEnv,
		Config: testEnv.GetConfig(),
		Scheme: testEnv.GetScheme(),
		Mapper: mapper,
		Generators: map[string]generators.GeneratorFactory{
			"List": list.GeneratorFactory,
			"Matrix": matrix.GeneratorFactory(map[string]generators.GeneratorFactory{
				"List":          list.GeneratorFactory,
				"GitRepository": gitrepository.GeneratorFactory,
				"PullRequests":  pullrequests.GeneratorFactory,
			}),
			"PullRequests": pullrequests.GeneratorFactory,
			"Cluster":      cluster.GeneratorFactory,
		},
		EventRecorder: testEnv.GetEventRecorderFor("gitopsset-controller"),
	}).SetupWithManager(testEnv); err != nil {
		panic(fmt.Sprintf("Failed to start GitOpsSetReconciler: %v", err))
	}

	go func() {
		fmt.Println("Starting the test environment")
		if err := testEnv.Start(ctx); err != nil {
			panic(fmt.Sprintf("Failed to start the test environment manager: %v", err))
		}
	}()
	<-testEnv.Manager.Elected()

	code := m.Run()

	fmt.Println("Stopping the test environment")
	if err := testEnv.Stop(); err != nil {
		panic(fmt.Sprintf("Failed to stop the test environment: %v", err))
	}

	os.Exit(code)
}
