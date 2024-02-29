package main

import (
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"github.com/fluxcd/pkg/http/fetch"
	runtimeclient "github.com/fluxcd/pkg/runtime/client"
	runtimeCtrl "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/events"
	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/fluxcd/pkg/runtime/metrics"
	"github.com/fluxcd/pkg/runtime/pprof"
	"github.com/fluxcd/pkg/tar"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators/apiclient"
	"github.com/gitops-tools/gitopssets-controller/pkg/setup"
	flag "github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

const (
	controllerName = "GitOpsSet"
	retries        = 9
)

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
	flag.StringSliceVar(&enabledGenerators, "enabled-generators", setup.DefaultGenerators, "Generators to enable.")

	logOptions.BindFlags(flag.CommandLine)
	clientOptions.BindFlags(flag.CommandLine)

	flag.Parse()

	watchNamespace := ""
	if !watchAllNamespaces {
		watchNamespace = os.Getenv("RUNTIME_NAMESPACE")
	}

	ctrl.SetLogger(logger.NewLogger(logOptions))

	setupLog.Info("configuring manager", "version", Version)

	err := setup.ValidateEnabledGenerators(enabledGenerators)
	if err != nil {
		setupLog.Error(err, "invalid enabled generators")
		os.Exit(1)
	}
	setupLog.Info("Enabled generators", "generators", enabledGenerators)

	scheme, err := setup.NewSchemeForGenerators(enabledGenerators)
	if err != nil {
		setupLog.Error(err, "unable to create scheme")
		os.Exit(1)
	}

	ctrlOptions := ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "539e4b66.weave.works",
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			ExtraHandlers: pprof.GetHandlers(),
		},
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
		Client: ctrlclient.Options{
			Cache: &ctrlclient.CacheOptions{
				DisableFor: []ctrlclient.Object{&corev1.Secret{}, &corev1.ConfigMap{}},
			},
		},
	}
	if watchNamespace != "" {
		ctrlOptions.Cache.DefaultNamespaces = map[string]ctrlcache.Config{
			watchNamespace: ctrlcache.Config{},
		}
	}

	restConfig := runtimeclient.GetConfigOrDie(clientOptions)
	mgr, err := ctrl.NewManager(restConfig, ctrlOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	httpClient, err := rest.HTTPClientFor(restConfig)
	if err != nil {
		setupLog.Error(err, "error creating httpClient using kubeconfig")
		os.Exit(1)
	}

	mapper, err := apiutil.NewDynamicRESTMapper(restConfig, httpClient)
	if err != nil {
		setupLog.Error(err, "unable to create REST Mapper")
		os.Exit(1)
	}
	metricsH := runtimeCtrl.NewMetrics(mgr, metrics.MustMakeRecorder(), templatesv1.GitOpsSetFinalizer)
	var eventRecorder *events.Recorder
	if eventRecorder, err = events.NewRecorder(mgr, ctrl.Log, eventsAddr, controllerName); err != nil {
		setupLog.Error(err, "unable to create event recorder")
		os.Exit(1)
	}

	fetcher := fetch.NewArchiveFetcher(retries, tar.UnlimitedUntarSize, tar.UnlimitedUntarSize, "")

	if err = (&controllers.GitOpsSetReconciler{
		Client:                mgr.GetClient(),
		DefaultServiceAccount: defaultServiceAccount,
		Config:                mgr.GetConfig(),
		Scheme:                mgr.GetScheme(),
		Mapper:                mapper,
		// TODO: Figure how to configure the DefaultClient.
		Generators:    setup.GetGenerators(enabledGenerators, fetcher, apiclient.DefaultClientFactory),
		Metrics:       metricsH,
		EventRecorder: eventRecorder,
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
