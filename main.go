package main

import (
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	runtimeclient "github.com/fluxcd/pkg/runtime/client"
	runtimeCtrl "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/logger"
	flag "github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	templatesv1alpha1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/cluster"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/gitrepository"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/list"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/matrix"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/pullrequests"

	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"

	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const controllerName = "GitOpsSet"

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	utilruntime.Must(clustersv1.AddToScheme(scheme))
	utilruntime.Must(templatesv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr           string
		enableLeaderElection  bool
		probeAddr             string
		watchAllNamespaces    bool
		defaultServiceAccount string
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

	logOptions.BindFlags(flag.CommandLine)
	clientOptions.BindFlags(flag.CommandLine)

	flag.Parse()

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
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	mapper, err := apiutil.NewDynamicRESTMapper(restConfig)
	if err != nil {
		setupLog.Error(err, "unable to create REST Mapper")
		os.Exit(1)
	}
	metricsH := runtimeCtrl.MustMakeMetrics(mgr)

	if err = (&controllers.GitOpsSetReconciler{
		Client:                mgr.GetClient(),
		DefaultServiceAccount: defaultServiceAccount,
		Config:                mgr.GetConfig(),
		Scheme:                mgr.GetScheme(),
		Mapper:                mapper,
		Generators: map[string]generators.GeneratorFactory{
			"List":          list.GeneratorFactory,
			"GitRepository": gitrepository.GeneratorFactory,
			"Matrix": matrix.GeneratorFactory(map[string]generators.GeneratorFactory{
				"List":          list.GeneratorFactory,
				"GitRepository": gitrepository.GeneratorFactory,
				"PullRequests":  pullrequests.GeneratorFactory,
				"Cluster":       cluster.GeneratorFactory,
			}),
			"PullRequests": pullrequests.GeneratorFactory,
			"Cluster":      cluster.GeneratorFactory,
		},
		Metrics: metricsH,
		// EventRecorder: eventRecorder,
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
