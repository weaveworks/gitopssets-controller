package main

import (
	"fmt"
	"net/http"
	"os"

	"golang.org/x/exp/slices"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	runtimeclient "github.com/fluxcd/pkg/runtime/client"
	runtimeCtrl "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/events"
	"github.com/fluxcd/pkg/runtime/logger"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	flag "github.com/spf13/pflag"
	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	templatesv1alpha1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/weaveworks/gitopssets-controller/controllers"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/apiclient"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/cluster"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/gitrepository"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/imagepolicy"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/list"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/matrix"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/pullrequests"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const controllerName = "GitOpsSet"

var allGenerators = []string{"GitRepository", "Cluster", "PullRequests", "List", "APIClient", "ImagePolicy", "Matrix"}
var defaultGenerators = []string{"GitRepository", "PullRequests", "List", "APIClient", "Matrix"}

func initScheme(enabledGenerators []string) {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	utilruntime.Must(templatesv1alpha1.AddToScheme(scheme))

	if isGeneratorEnabled(enabledGenerators, "Cluster") {
		utilruntime.Must(clustersv1.AddToScheme(scheme))
	}

	if isGeneratorEnabled(enabledGenerators, "ImagePolicy") {
		utilruntime.Must(imagev1.AddToScheme(scheme))
	}
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr           string
		enableLeaderElection  bool
		probeAddr             string
		watchAllNamespaces    bool
		defaultServiceAccount string
		enabledGenerators     []string
		clientOptions         runtimeclient.Options
		logOptions            logger.Options
		eventsAddr            string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&eventsAddr, "events-addr", "", "The address of the events receiver.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&watchAllNamespaces, "watch-all-namespaces", true,
		"Watch for custom resources in all namespaces, if set to false it will only watch the runtime namespace.")
	flag.StringVar(&defaultServiceAccount, "default-service-account", "", "Default service account used for impersonation.")
	flag.StringSliceVar(&enabledGenerators, "enabled-generators", defaultGenerators, "Generators to enable.")

	logOptions.BindFlags(flag.CommandLine)
	clientOptions.BindFlags(flag.CommandLine)

	flag.Parse()

	err := validateEnabledGenerators(enabledGenerators)
	if err != nil {
		setupLog.Error(err, "invalid enabled generators")
		os.Exit(1)
	}
	setupLog.Info("Enabled generators", "generators", enabledGenerators)

	initScheme(enabledGenerators)

	watchNamespace := ""
	if !watchAllNamespaces {
		watchNamespace = os.Getenv("RUNTIME_NAMESPACE")
	}

	ctrl.SetLogger(logger.NewLogger(logOptions))
	restConfig := runtimeclient.GetConfigOrDie(clientOptions)
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		Namespace:              watchNamespace,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "539e4b66.weave.works",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,

		// Don't cache Secrets and ConfigMaps. In general, the
		// controller-runtime client does a LIST and WATCH to cache
		// kinds you request (see
		// https://github.com/kubernetes-sigs/controller-runtime/pull/1249),
		// and this can mean caching all secrets and configmaps; when
		// all that's required is the few that are referenced for
		// objects of interest to this controller.
		ClientDisableCacheFor: []ctrlclient.Object{&corev1.Secret{}, &corev1.ConfigMap{}},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	mapper, err := apiutil.NewDynamicRESTMapper(restConfig, http.DefaultClient)
	if err != nil {
		setupLog.Error(err, "unable to create REST Mapper")
		os.Exit(1)
	}
	metricsH := runtimeCtrl.MustMakeMetrics(mgr)
	var eventRecorder *events.Recorder
	if eventRecorder, err = events.NewRecorder(mgr, ctrl.Log, eventsAddr, controllerName); err != nil {
		setupLog.Error(err, "unable to create event recorder")
		os.Exit(1)
	}

	if err = (&controllers.GitOpsSetReconciler{
		Client:                mgr.GetClient(),
		DefaultServiceAccount: defaultServiceAccount,
		Config:                mgr.GetConfig(),
		Scheme:                mgr.GetScheme(),
		Mapper:                mapper,
		Generators:            getGenerators(enabledGenerators),
		Metrics:               metricsH,
		EventRecorder:         eventRecorder,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", controllerName)
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func validateEnabledGenerators(enabledGenerators []string) error {
	for _, generator := range enabledGenerators {
		if !slices.Contains(allGenerators, generator) {
			return fmt.Errorf("invalid generator %q. valid values: %q", generator, allGenerators)
		}
	}
	return nil
}

func getGenerators(enabledGenerators []string) map[string]generators.GeneratorFactory {
	matrixGenerators := filterEnabledGenerators(enabledGenerators, map[string]generators.GeneratorFactory{
		"List":          list.GeneratorFactory,
		"GitRepository": gitrepository.GeneratorFactory,
		"PullRequests":  pullrequests.GeneratorFactory,
		"Cluster":       cluster.GeneratorFactory,
		"ImagePolicy":   imagepolicy.GeneratorFactory,
		// TODO: Figure out how to configure the client
		"APIClient": apiclient.GeneratorFactory(http.DefaultClient),
	})

	return filterEnabledGenerators(enabledGenerators, map[string]generators.GeneratorFactory{
		"List":          list.GeneratorFactory,
		"GitRepository": gitrepository.GeneratorFactory,
		"PullRequests":  pullrequests.GeneratorFactory,
		"Cluster":       cluster.GeneratorFactory,
		// TODO: Figure out how to configure the client
		"APIClient":   apiclient.GeneratorFactory(http.DefaultClient),
		"ImagePolicy": imagepolicy.GeneratorFactory,
		"Matrix":      matrix.GeneratorFactory(matrixGenerators),
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
