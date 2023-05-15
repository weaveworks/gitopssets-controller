package imagepolicy

import (
	"context"
	"fmt"
	"time"

	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	"github.com/go-logr/logr"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ImagePolicyGenerator extracts files from Flux ImagePolicy resources.
type ImagePolicyGenerator struct {
	client.Client
	logr.Logger
}

// GeneratorFactory is a function for creating per-reconciliation generators for
// the ImagePolicyGenerator.
func GeneratorFactory(l logr.Logger, c client.Client) generators.Generator {
	return NewGenerator(l, c)
}

// NewGenerator creates and returns a new ImagePolicy generator.
func NewGenerator(l logr.Logger, c client.Client) *ImagePolicyGenerator {
	return &ImagePolicyGenerator{
		Client: c,
		Logger: l,
	}
}

// Generate is an implementation of the Generator interface.
//
// This uses the referenced Flux ImagePolicy to determine the images to
// return.
func (g *ImagePolicyGenerator) Generate(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	if sg == nil {
		return nil, generators.ErrEmptyGitOpsSet
	}
	if sg.ImagePolicy == nil {
		return nil, nil
	}

	g.Logger.Info("generating params from ImagePolicy generator", "repo", sg.ImagePolicy.PolicyRef)

	var repo imagev1.ImagePolicy
	repoName := client.ObjectKey{Name: sg.ImagePolicy.PolicyRef, Namespace: ks.GetNamespace()}
	if err := g.Client.Get(ctx, repoName, &repo); err != nil {
		return nil, fmt.Errorf("could not load ImagePolicy: %w", err)
	}

	result := []map[string]any{}

	if repo.Status.LatestImage == "" {
		g.Logger.Info("image policy has not calculated the latest image")
		return result, nil
	}

	g.Logger.Info("image policy", "latestImage", repo.Status.LatestImage, "previousImage", repo.Status.ObservedPreviousImage)

	result = append(result, map[string]any{
		"latestImage":   repo.Status.LatestImage,
		"previousImage": repo.Status.ObservedPreviousImage,
	})

	return result, nil
}

// Interval is an implementation of the Generator interface.
//
// ImagePolicyGenerator is driven by watching a Flux ImagePolicy resource.
func (g *ImagePolicyGenerator) Interval(sg *templatesv1.GitOpsSetGenerator) time.Duration {
	return generators.NoRequeueInterval
}
