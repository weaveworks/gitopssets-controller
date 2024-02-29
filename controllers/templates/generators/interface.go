package generators

import (
	"context"
	"errors"
	"time"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GeneratorFactory is a way to create a per-reconciliation generator.
type GeneratorFactory func(logr.Logger, client.Reader) Generator

// Generator defines the interface implemented by all GitOpsSet generators.
type Generator interface {
	// Generate interprets the GitOpsSet and generates all relevant
	// parameters for the GitOpsSet template.
	// The expected / desired list of parameters is returned, it then will be render and reconciled
	// against the current state of the Applications in the cluster.
	Generate(context.Context, *templatesv1.GitOpsSetGenerator, *templatesv1.GitOpsSet) ([]map[string]any, error)

	// Interval is the the generator can controller the next reconciled loop
	//
	// In case there is more then one generator the time will be the minimum of the times.
	// In case NoRequeueInterval is empty, it will be ignored
	Interval(*templatesv1.GitOpsSetGenerator) time.Duration
}

// ErrEmptyGitOpsSetGenerator is returned when GitOpsSet is
// empty.
var ErrEmptyGitOpsSet = errors.New("GitOpsSet is empty")

var NoRequeueInterval time.Duration

// DefaultInterval is used when Interval is not specified, it
// is the default time to wait before the next reconcile loop.
const DefaultRequeueAfterSeconds = 3 * time.Minute
